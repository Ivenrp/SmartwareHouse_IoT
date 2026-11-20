package mqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"smart-warehouse-backend/config"
	"smart-warehouse-backend/db"
	"smart-warehouse-backend/models"
	"strconv"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type MQTTService struct {
	client         paho.Client
	cfg            *config.Config
	latestTele     models.Telemetry
	latestStatus   string
	lastSeen       time.Time
	mu             sync.RWMutex
	commandHandler func(cmd models.MQTTCommandPayload)
	offlineAlarmFired bool
}

var Instance *MQTTService

func NewMQTTService(cfg *config.Config) *MQTTService {
	s := &MQTTService{
		cfg:          cfg,
		latestStatus: "offline",
		lastSeen:     time.Now(),
		latestTele: models.Telemetry{
			DeviceID:      "lift01",
			CurrentLevel:  1,
			DistanceCm:    5.0,
			WeightG:       0.0,
			Estop:         false,
			FlowState:     "IDLE",
			LiftStatus:    "STOPPED",
			TravelTimeSec: 0.0,
			Enabled:       true,
			Mode:          "AUTO",
			RecordedAt:    time.Now(),
		},
	}
	Instance = s
	return s
}

func (s *MQTTService) Start() {
	if s.cfg.MQTTBroker != "" {
		opts := paho.NewClientOptions()
		opts.AddBroker(s.cfg.MQTTBroker)
		opts.SetClientID(s.cfg.MQTTClientID + "-" + strconv.FormatInt(time.Now().Unix(), 10))
		if s.cfg.MQTTUsername != "" {
			opts.SetUsername(s.cfg.MQTTUsername)
			opts.SetPassword(s.cfg.MQTTPassword)
		}
		opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
		opts.SetAutoReconnect(true)
		opts.SetConnectRetry(true)
		opts.SetConnectRetryInterval(5 * time.Second)

		opts.OnConnect = func(c paho.Client) {
			log.Println("[MQTT] Connected to broker:", s.cfg.MQTTBroker)
			s.subscribeTopics(c)
		}

		opts.OnConnectionLost = func(c paho.Client, err error) {
			log.Printf("[MQTT] Connection lost: %v. Reconnecting...", err)
		}

		client := paho.NewClient(opts)
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			log.Printf("[MQTT] Warning: Could not connect to MQTT broker (%v). Running with internal virtual state.", token.Error())
		} else {
			s.client = client
		}
	} else {
		log.Println("[MQTT] No external broker specified. Running embedded virtual state.")
	}

	// Start Heartbeat & Offline Watchdog (Check every 2 seconds)
	go s.watchdog()
}

func (s *MQTTService) subscribeTopics(c paho.Client) {
	// Telemetry topic (QoS 0)
	c.Subscribe("warehouse/lift01/telemetry", 0, func(c paho.Client, m paho.Message) {
		var tele models.MQTTTelemetryPayload
		if err := json.Unmarshal(m.Payload(), &tele); err == nil {
			s.HandleTelemetry(tele)
		}
	})

	// Event topic (QoS 1)
	c.Subscribe("warehouse/lift01/event", 1, func(c paho.Client, m paho.Message) {
		var evt models.MQTTEventPayload
		if err := json.Unmarshal(m.Payload(), &evt); err == nil {
			s.HandleEvent(evt)
		}
	})

	// Status topic (QoS 1, retained)
	c.Subscribe("warehouse/lift01/status", 1, func(c paho.Client, m paho.Message) {
		var st struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(m.Payload(), &st); err == nil {
			s.mu.Lock()
			s.latestStatus = st.Status
			s.mu.Unlock()
		}
	})
}

func (s *MQTTService) PublishCommand(cmd models.MQTTCommandPayload) error {
	cmd.Timestamp = time.Now().UTC().Format(time.RFC3339)

	// If simulator is active and handles commands
	if s.commandHandler != nil {
		go s.commandHandler(cmd)
	}

	if s.client != nil && s.client.IsConnected() {
		data, err := json.Marshal(cmd)
		if err != nil {
			return err
		}
		token := s.client.Publish("warehouse/lift01/command", 1, false, data)
		token.Wait()
		return token.Error()
	}

	return nil
}

func (s *MQTTService) SetCommandHandler(h func(cmd models.MQTTCommandPayload)) {
	s.commandHandler = h
}

func (s *MQTTService) HandleTelemetry(p models.MQTTTelemetryPayload) {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.latestStatus = "online"
	s.offlineAlarmFired = false

	s.latestTele = models.Telemetry{
		DeviceID:      p.DeviceID,
		CurrentLevel:  p.CurrentLevel,
		DistanceCm:    p.DistanceCm,
		WeightG:       p.WeightG,
		Estop:         p.Estop,
		FlowState:     p.FlowState,
		LiftStatus:    p.LiftStatus,
		TravelTimeSec: p.TravelTimeSec,
		Enabled:       p.Enabled,
		Mode:          p.Mode,
		CommandID:     p.CommandID,
		RecordedAt:    time.Now(),
	}
	tCopy := s.latestTele
	s.mu.Unlock()

	// Persist telemetry into database periodically
	go func() {
		_ = db.SaveTelemetry(&tCopy)
	}()
}

func (s *MQTTService) HandleEvent(evt models.MQTTEventPayload) {
	log.Printf("[MQTT Event] CommandID: %s, Result: %s, FinalLevel: %d, Weight: %.2fg",
		evt.CommandID, evt.Result, evt.FinalLevel, evt.FinalWeightG)

	if evt.CommandID == "" {
		return
	}

	cmd, err := db.GetCommandByID(evt.CommandID)
	if err != nil || cmd == nil {
		log.Printf("[MQTT Event] Command %s not found in DB", evt.CommandID)
		return
	}

	if evt.Result == "COMPLETE" {
		// Weight validation against master item (PRD 6.3 rule 4)
		if cmd.UnitID != nil {
			unit, _ := db.GetUnitByID(*cmd.UnitID)
			if unit != nil {
				item, _ := db.GetItemByID(unit.ItemID)
				if item != nil && item.ExpectedWeightG > 0 {
					// calculate tolerance
					tolerance := item.ExpectedWeightG * (item.WeightTolerancePct / 100.0)
					diff := evt.FinalWeightG - item.ExpectedWeightG
					if diff < 0 {
						diff = -diff
					}

					if diff > tolerance {
						// Alarm WEIGHT_MISMATCH
						reason := fmt.Sprintf("Berat sensor (%.2fg) melampaui toleransi %.1f%% dari acuan %.2fg",
							evt.FinalWeightG, item.WeightTolerancePct, item.ExpectedWeightG)
						_ = db.UpdateCommandStatus(evt.CommandID, "FAILED", &reason)
						_ = db.CreateAlarm(&models.Alarm{
							Type:      "WEIGHT_MISMATCH",
							Message:   reason,
							CommandID: evt.CommandID,
						})
						_ = db.UpdateUnitStatus(unit.ID, "FAILED", nil, nil)
						return
					}
				}

				// If STORE action
				if cmd.Action == "STORE" {
					_ = db.UpdateUnitStatus(unit.ID, "STORED", &evt.FinalLevel, &evt.FinalWeightG)
					_ = db.RecordMovement(&models.StockMovement{
						UnitID:    &unit.ID,
						CommandID: evt.CommandID,
						Type:      "IN",
						Level:     &evt.FinalLevel,
						WeightG:   &evt.FinalWeightG,
						Status:    "SUCCESS",
						Note:      "Simpan barang via QR scan",
					})
				} else if cmd.Action == "RETRIEVE" {
					_ = db.UpdateUnitStatus(unit.ID, "OUT", nil, nil)
					_ = db.RecordMovement(&models.StockMovement{
						UnitID:    &unit.ID,
						CommandID: evt.CommandID,
						Type:      "OUT",
						Level:     &evt.FinalLevel,
						WeightG:   &evt.FinalWeightG,
						Status:    "SUCCESS",
						Note:      "Ambil barang ke level 1",
					})
				}
			}
		}

		_ = db.UpdateCommandStatus(evt.CommandID, "COMPLETE", nil)

	} else if evt.Result == "FAILED" {
		reason := "Transaction failed"
		if evt.FaultReason != nil && *evt.FaultReason != "" {
			reason = *evt.FaultReason
		}
		_ = db.UpdateCommandStatus(evt.CommandID, "FAILED", &reason)
		_ = db.CreateAlarm(&models.Alarm{
			Type:      "LIFT_FAULT",
			Message:   "Perintah lift gagal: " + reason,
			CommandID: evt.CommandID,
		})

		if cmd.UnitID != nil {
			_ = db.UpdateUnitStatus(*cmd.UnitID, "FAILED", nil, nil)
		}
	}
}

func (s *MQTTService) watchdog() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		elapsed := time.Since(s.lastSeen)
		if elapsed > 10*time.Second {
			s.latestStatus = "offline"
			if !s.offlineAlarmFired {
				s.offlineAlarmFired = true
				go func() {
					_ = db.CreateAlarm(&models.Alarm{
						Type:      "DEVICE_OFFLINE",
						Message:   "Perangkat ESP32 offline (tidak ada heartbeat > 10 detik)",
						CommandID: "",
					})
				}()
			}
		} else {
			s.latestStatus = "online"
		}
		s.mu.Unlock()
	}
}

func (s *MQTTService) GetCurrentState() (models.Telemetry, string, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestTele, s.latestStatus, s.lastSeen
}

func (s *MQTTService) UpdateStateDirectly(tele models.Telemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = time.Now()
	s.latestStatus = "online"
	s.latestTele = tele
}
