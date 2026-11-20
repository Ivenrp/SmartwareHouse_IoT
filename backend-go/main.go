package main

import (
	"log"
	"net/http"
	"smart-warehouse-backend/config"
	"smart-warehouse-backend/db"
	"smart-warehouse-backend/handlers"
	"smart-warehouse-backend/mqtt"
	"smart-warehouse-backend/simulator"
)

func main() {
	log.Println("==================================================")
	log.Println(" SMART WAREHOUSE - GOLANG BACKEND API & MQTT CLIENT")
	log.Println("==================================================")

	cfg := config.LoadConfig()

	// 1. Connect to Database
	if _, err := db.InitDB(cfg); err != nil {
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}

	// 2. Start MQTT Service (HiveMQ Cloud client)
	mqttSvc := mqtt.NewMQTTService(cfg)
	mqttSvc.Start()

	// 3. Start Hardware Simulator if enabled
	if cfg.EnableSimulator {
		sim := simulator.NewLiftSimulator()
		sim.Start(mqttSvc)
	}

	// 4. Setup REST API Routes
	api := handlers.NewAPIHandler(cfg, mqttSvc)
	mux := http.NewServeMux()

	// Lift telemetry and control
	mux.HandleFunc("GET /api/v1/lift/status", api.AuthMiddleware(api.GetLiftStatus))
	mux.HandleFunc("GET /api/v1/lift/telemetry", api.AuthMiddleware(api.GetLiftTelemetry))
	mux.HandleFunc("POST /api/v1/commands", api.AuthMiddleware(api.CreateCommand))
	mux.HandleFunc("GET /api/v1/commands/{id}", api.AuthMiddleware(api.GetCommand))
	mux.HandleFunc("POST /api/v1/lift/stop", api.AuthMiddleware(api.StopLift))
	mux.HandleFunc("POST /api/v1/lift/reset", api.AuthMiddleware(api.ResetLift))
	mux.HandleFunc("POST /api/v1/lift/enable", api.AuthMiddleware(api.EnableLift))
	mux.HandleFunc("POST /api/v1/lift/disable", api.AuthMiddleware(api.DisableLift))
	mux.HandleFunc("POST /api/v1/lift/mode", api.AuthMiddleware(api.SetLiftMode))
	mux.HandleFunc("POST /api/v1/lift/move", api.AuthMiddleware(api.ManualMove))
	mux.HandleFunc("POST /api/v1/lift/jog", api.AuthMiddleware(api.JogLift))
	mux.HandleFunc("POST /api/v1/lift/home", api.AuthMiddleware(api.HomeLift))

	// Items & Units
	mux.HandleFunc("GET /api/v1/items", api.AuthMiddleware(api.GetItems))
	mux.HandleFunc("POST /api/v1/items", api.AuthMiddleware(api.CreateItem))
	mux.HandleFunc("PUT /api/v1/items/{id}", api.AuthMiddleware(api.UpdateItem))
	mux.HandleFunc("POST /api/v1/items/{id}/units", api.AuthMiddleware(api.GenerateUnits))
	mux.HandleFunc("GET /api/v1/units/{qr}", api.AuthMiddleware(api.GetUnitByQR))

	// Stock & Movements
	mux.HandleFunc("GET /api/v1/stock/summary", api.AuthMiddleware(api.GetStockSummary))
	mux.HandleFunc("GET /api/v1/stock/levels", api.AuthMiddleware(api.GetStockLevels))
	mux.HandleFunc("POST /api/v1/stock/adjust", api.AuthMiddleware(api.AdjustStock))
	mux.HandleFunc("GET /api/v1/movements", api.AuthMiddleware(api.GetMovements))

	// Alarms & Settings
	mux.HandleFunc("GET /api/v1/alarms", api.AuthMiddleware(api.GetAlarms))
	mux.HandleFunc("PATCH /api/v1/alarms/{id}/ack", api.AuthMiddleware(api.AckAlarm))
	mux.HandleFunc("GET /api/v1/settings", api.AuthMiddleware(api.GetSettings))
	mux.HandleFunc("PUT /api/v1/settings", api.AuthMiddleware(api.UpdateSettings))

	// Healthcheck endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"OK","service":"smart-warehouse-backend"}`))
	})

	serverAddr := ":" + cfg.Port
	log.Printf("[Server] Starting REST API server on http://127.0.0.1%s", serverAddr)
	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		log.Fatalf("[FATAL] Server error: %v", err)
	}
}
