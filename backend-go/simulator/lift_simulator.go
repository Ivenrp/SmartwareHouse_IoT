package simulator

import (
	"log"
	"math/rand"
	"smart-warehouse-backend/models"
	"smart-warehouse-backend/mqtt"
	"sync"
	"time"
)

type LiftSimulator struct {
	mu            sync.Mutex
	currentLevel  int
	distanceCm    float64
	weightG       float64
	estop         bool
	flowState     string
	liftStatus    string
	travelTimeSec float64
	enabled       bool
	mode          string
	activeCmdID   string
	activeTarget  int
	activeAction  string
	expectedWeight float64
	maxLoad       float64
	stopCh        chan struct{}
	jogTimer      *time.Timer
}

func NewLiftSimulator() *LiftSimulator {
	return &LiftSimulator{
		currentLevel:  1,
		distanceCm:    5.0,
		weightG:       0.0,
		estop:         false,
		flowState:     "IDLE",
		liftStatus:    "STOPPED",
		travelTimeSec: 0.0,
		enabled:       true,
		mode:          "AUTO",
		maxLoad:       800.0,
		stopCh:        make(chan struct{}),
	}
}

func (sim *LiftSimulator) Start(mqttSvc *mqtt.MQTTService) {
	log.Println("[Simulator] ESP32 Lift Hardware Simulator started")

	// Register command handler with MQTT Service
	mqttSvc.SetCommandHandler(func(cmd models.MQTTCommandPayload) {
		sim.ExecuteCommand(cmd, mqttSvc)
	})

	// Run periodic sensor telemetry publisher (1 per second)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				sim.mu.Lock()
				// Add tiny sensor noise for realism
				jitter := (rand.Float64() - 0.5) * 0.2
				dist := sim.distanceCm + jitter
				if dist < 0 {
					dist = 0
				}
				wJitter := (rand.Float64() - 0.5) * 0.3
				w := sim.weightG
				if w > 0 {
					w += wJitter
				}

				tele := models.MQTTTelemetryPayload{
					DeviceID:      "lift01",
					CurrentLevel:  sim.currentLevel,
					DistanceCm:    roundFloat(dist, 1),
					WeightG:       roundFloat(w, 1),
					Estop:         sim.estop,
					FlowState:     sim.flowState,
					LiftStatus:    sim.liftStatus,
					TravelTimeSec: roundFloat(sim.travelTimeSec, 1),
					CommandID:     sim.activeCmdID,
					Enabled:       sim.enabled,
					Mode:          sim.mode,
					Timestamp:     time.Now().UTC().Format(time.RFC3339),
				}
				sim.mu.Unlock()

				mqttSvc.HandleTelemetry(tele)
			case <-sim.stopCh:
				return
			}
		}
	}()
}

func (sim *LiftSimulator) ExecuteCommand(cmd models.MQTTCommandPayload, mqttSvc *mqtt.MQTTService) {
	sim.mu.Lock()
	defer sim.mu.Unlock()

	log.Printf("[Simulator] Received Action: %s, CmdID: %s, TargetLevel: %d", cmd.Action, cmd.CommandID, cmd.TargetLevel)

	switch cmd.Action {
	case "ENABLE":
		sim.enabled = true
		log.Println("[Simulator] Lift motor driver enabled")

	case "DISABLE":
		if sim.flowState == "IDLE" {
			sim.enabled = false
			log.Println("[Simulator] Lift motor driver disabled")
		}

	case "SET_MODE":
		if sim.flowState == "IDLE" {
			sim.mode = cmd.Mode
			log.Printf("[Simulator] Mode changed to %s", sim.mode)
		}

	case "STOP":
		sim.flowState = "IDLE"
		sim.liftStatus = "STOPPED"
		sim.travelTimeSec = 0
		sim.activeCmdID = ""
		log.Println("[Simulator] Emergency STOP triggered")

	case "RESET":
		if !sim.estop {
			sim.flowState = "IDLE"
			sim.liftStatus = "STOPPED"
			sim.travelTimeSec = 0
			sim.activeCmdID = ""
			log.Println("[Simulator] FAULT reset successfully")
		}

	case "JOG":
		if !sim.enabled || sim.estop || sim.mode != "MANUAL" {
			return
		}
		if cmd.Direction == "UP" {
			sim.liftStatus = "UP"
			if sim.distanceCm < 20.0 {
				sim.distanceCm += 0.5
			}
		} else if cmd.Direction == "DOWN" {
			sim.liftStatus = "DOWN"
			if sim.distanceCm > 4.5 {
				sim.distanceCm -= 0.5
			}
		}

		if sim.jogTimer != nil {
			sim.jogTimer.Stop()
		}
		// PRD Section 7.7 FR-65 / Section 9: Jog stops if no pulse for 500ms
		sim.jogTimer = time.AfterFunc(500*time.Millisecond, func() {
			sim.mu.Lock()
			if sim.liftStatus == "UP" || sim.liftStatus == "DOWN" {
				sim.liftStatus = "STOPPED"
			}
			sim.mu.Unlock()
		})

	case "HOME":
		if !sim.enabled || sim.estop {
			return
		}
		sim.activeCmdID = cmd.CommandID
		sim.flowState = "MOVING"
		sim.liftStatus = "DOWN"
		go sim.runHomingSequence(cmd.CommandID, mqttSvc)

	case "MOVE":
		if !sim.enabled || sim.estop {
			return
		}
		sim.activeCmdID = cmd.CommandID
		sim.activeTarget = cmd.TargetLevel
		sim.flowState = "MOVING"
		if cmd.TargetLevel > sim.currentLevel {
			sim.liftStatus = "UP"
		} else {
			sim.liftStatus = "DOWN"
		}
		go sim.runMoveSequence(cmd.CommandID, cmd.TargetLevel, mqttSvc)

	case "STORE":
		if !sim.enabled || sim.estop || sim.mode != "AUTO" {
			return
		}
		// Interlock overload check
		if cmd.ExpectedWeightG > cmd.MaxLoadG && cmd.MaxLoadG > 0 {
			sim.flowState = "IDLE"
			sim.liftStatus = "FAULT"
			reason := "OVERLOAD: berat melebihi batas maksimal load cell"
			go func() {
				mqttSvc.HandleEvent(models.MQTTEventPayload{
					CommandID:   cmd.CommandID,
					Result:      "FAILED",
					FaultReason: &reason,
				})
			}()
			return
		}

		sim.activeCmdID = cmd.CommandID
		sim.activeTarget = cmd.TargetLevel
		sim.activeAction = "STORE"
		sim.expectedWeight = cmd.ExpectedWeightG
		sim.weightG = cmd.ExpectedWeightG // simulate placing object on load cell

		go sim.runStoreSequence(cmd, mqttSvc)

	case "RETRIEVE":
		if !sim.enabled || sim.estop || sim.mode != "AUTO" {
			return
		}
		sim.activeCmdID = cmd.CommandID
		sim.activeTarget = 1
		sim.activeAction = "RETRIEVE"

		go sim.runRetrieveSequence(cmd, mqttSvc)
	}
}

func (sim *LiftSimulator) runStoreSequence(cmd models.MQTTCommandPayload, mqttSvc *mqtt.MQTTService) {
	// Step 1: VALIDASI (operator places item, sensor stabilizes)
	sim.mu.Lock()
	sim.flowState = "VALIDASI"
	sim.liftStatus = "STOPPED"
	sim.travelTimeSec = 0.5
	sim.mu.Unlock()
	time.Sleep(1 * time.Second)

	// Step 2: MOVING (cabin moves to target level)
	targetDist := 5.0
	if cmd.TargetLevel == 2 {
		targetDist = 18.5
	}

	sim.mu.Lock()
	sim.flowState = "MOVING"
	if targetDist > sim.distanceCm {
		sim.liftStatus = "UP"
	} else {
		sim.liftStatus = "DOWN"
	}
	sim.mu.Unlock()

	// Simulate motor steps over 3 seconds
	steps := 6
	stepDelay := 500 * time.Millisecond
	for i := 0; i < steps; i++ {
		time.Sleep(stepDelay)
		sim.mu.Lock()
		if sim.estop || sim.liftStatus == "STOPPED" {
			sim.mu.Unlock()
			return
		}
		sim.travelTimeSec += 0.5
		diff := (targetDist - sim.distanceCm) / float64(steps-i)
		sim.distanceCm += diff
		sim.mu.Unlock()
	}

	// Step 3: VERIFY (Limit switch triggered, HC-SR04 verifies target position)
	sim.mu.Lock()
	sim.flowState = "VERIFY"
	sim.liftStatus = "BRAKING"
	sim.distanceCm = targetDist
	sim.currentLevel = cmd.TargetLevel
	sim.mu.Unlock()
	time.Sleep(800 * time.Millisecond)

	// Step 4: COMPLETE
	sim.mu.Lock()
	sim.flowState = "COMPLETE"
	sim.liftStatus = "STOPPED"
	finalWeight := sim.weightG
	finalLevel := sim.currentLevel
	cmdID := sim.activeCmdID
	sim.mu.Unlock()

	mqttSvc.HandleEvent(models.MQTTEventPayload{
		CommandID:    cmdID,
		Result:       "COMPLETE",
		FinalLevel:   finalLevel,
		FinalWeightG: roundFloat(finalWeight, 1),
	})

	time.Sleep(1 * time.Second)
	sim.mu.Lock()
	sim.flowState = "IDLE"
	sim.travelTimeSec = 0
	sim.activeCmdID = ""
	sim.mu.Unlock()
}

func (sim *LiftSimulator) runRetrieveSequence(cmd models.MQTTCommandPayload, mqttSvc *mqtt.MQTTService) {
	// First move up to retrieve shelf if not already there
	sim.mu.Lock()
	sim.flowState = "VALIDASI"
	sim.mu.Unlock()
	time.Sleep(600 * time.Millisecond)

	sim.mu.Lock()
	sim.flowState = "MOVING"
	sim.liftStatus = "DOWN"
	sim.weightG = 248.5 // load is retrieved
	sim.mu.Unlock()

	// Move down to floor 1
	for i := 0; i < 6; i++ {
		time.Sleep(500 * time.Millisecond)
		sim.mu.Lock()
		if sim.estop || sim.liftStatus == "STOPPED" {
			sim.mu.Unlock()
			return
		}
		sim.travelTimeSec += 0.5
		if sim.distanceCm > 5.0 {
			sim.distanceCm -= 2.2
			if sim.distanceCm < 5.0 {
				sim.distanceCm = 5.0
			}
		}
		sim.mu.Unlock()
	}

	sim.mu.Lock()
	sim.flowState = "VERIFY"
	sim.liftStatus = "BRAKING"
	sim.distanceCm = 5.0
	sim.currentLevel = 1
	sim.mu.Unlock()
	time.Sleep(600 * time.Millisecond)

	sim.mu.Lock()
	sim.flowState = "COMPLETE"
	sim.liftStatus = "STOPPED"
	cmdID := sim.activeCmdID
	finalWeight := sim.weightG
	sim.weightG = 0.0 // item picked by operator
	sim.mu.Unlock()

	mqttSvc.HandleEvent(models.MQTTEventPayload{
		CommandID:    cmdID,
		Result:       "COMPLETE",
		FinalLevel:   1,
		FinalWeightG: roundFloat(finalWeight, 1),
	})

	time.Sleep(1 * time.Second)
	sim.mu.Lock()
	sim.flowState = "IDLE"
	sim.travelTimeSec = 0
	sim.activeCmdID = ""
	sim.mu.Unlock()
}

func (sim *LiftSimulator) runMoveSequence(cmdID string, targetLevel int, mqttSvc *mqtt.MQTTService) {
	targetDist := 5.0
	if targetLevel == 2 {
		targetDist = 18.5
	}

	for i := 0; i < 5; i++ {
		time.Sleep(500 * time.Millisecond)
		sim.mu.Lock()
		if sim.estop || sim.liftStatus == "STOPPED" {
			sim.mu.Unlock()
			return
		}
		sim.travelTimeSec += 0.5
		diff := (targetDist - sim.distanceCm) / float64(5-i)
		sim.distanceCm += diff
		sim.mu.Unlock()
	}

	sim.mu.Lock()
	sim.currentLevel = targetLevel
	sim.distanceCm = targetDist
	sim.flowState = "IDLE"
	sim.liftStatus = "STOPPED"
	sim.travelTimeSec = 0
	sim.activeCmdID = ""
	sim.mu.Unlock()

	mqttSvc.HandleEvent(models.MQTTEventPayload{
		CommandID:    cmdID,
		Result:       "COMPLETE",
		FinalLevel:   targetLevel,
		FinalWeightG: 0,
	})
}

func (sim *LiftSimulator) runHomingSequence(cmdID string, mqttSvc *mqtt.MQTTService) {
	for i := 0; i < 6; i++ {
		time.Sleep(400 * time.Millisecond)
		sim.mu.Lock()
		if sim.estop || sim.liftStatus == "STOPPED" {
			sim.mu.Unlock()
			return
		}
		sim.travelTimeSec += 0.4
		if sim.distanceCm > 5.0 {
			sim.distanceCm -= 2.0
			if sim.distanceCm < 5.0 {
				sim.distanceCm = 5.0
			}
		}
		sim.mu.Unlock()
	}

	sim.mu.Lock()
	sim.currentLevel = 1
	sim.distanceCm = 5.0
	sim.flowState = "IDLE"
	sim.liftStatus = "STOPPED"
	sim.travelTimeSec = 0
	sim.activeCmdID = ""
	sim.mu.Unlock()

	mqttSvc.HandleEvent(models.MQTTEventPayload{
		CommandID:    cmdID,
		Result:       "COMPLETE",
		FinalLevel:   1,
		FinalWeightG: 0,
	})
}

func roundFloat(val float64, precision uint) float64 {
	ratio := 1.0
	for i := uint(0); i < precision; i++ {
		ratio *= 10
	}
	return float64(int(val*ratio+0.5)) / ratio
}
