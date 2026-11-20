package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"smart-warehouse-backend/config"
	"smart-warehouse-backend/db"
	"smart-warehouse-backend/models"
	"smart-warehouse-backend/mqtt"
	"strconv"
	"strings"
	"time"
)

type APIHandler struct {
	cfg     *config.Config
	mqttSvc *mqtt.MQTTService
}

func NewAPIHandler(cfg *config.Config, mqttSvc *mqtt.MQTTService) *APIHandler {
	return &APIHandler{
		cfg:     cfg,
		mqttSvc: mqttSvc,
	}
}

// Helper to write JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}, apiErr *models.APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(models.APIResponse{
		Data:  data,
		Error: apiErr,
	})
}

func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, data, nil)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, nil, &models.APIError{
		Code:    code,
		Message: msg,
	})
}

// Bearer Token Auth Middleware (PRD 9.3)
func (h *APIHandler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// CORS Headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "FORBIDDEN", "Authorization header is required")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != h.cfg.APIToken {
			writeError(w, http.StatusUnauthorized, "FORBIDDEN", "Invalid or expired service token")
			return
		}

		next(w, r)
	}
}

// ---------------------- LIFT STATUS & CONTROL ----------------------

// GET /api/v1/lift/status
func (h *APIHandler) GetLiftStatus(w http.ResponseWriter, r *http.Request) {
	tele, status, lastSeen := h.mqttSvc.GetCurrentState()
	writeSuccess(w, map[string]interface{}{
		"device_id":        tele.DeviceID,
		"status":           status, // "online" or "offline"
		"last_seen":        lastSeen.Format(time.RFC3339),
		"current_level":    tele.CurrentLevel,
		"distance_cm":      tele.DistanceCm,
		"weight_g":         tele.WeightG,
		"estop":            tele.Estop,
		"flow_state":       tele.FlowState,
		"lift_status":      tele.LiftStatus,
		"travel_time_sec":  tele.TravelTimeSec,
		"enabled":          tele.Enabled,
		"mode":             tele.Mode,
		"active_command_id": tele.CommandID,
		"recorded_at":      tele.RecordedAt.Format(time.RFC3339),
	})
}

// GET /api/v1/lift/telemetry?minutes=30
func (h *APIHandler) GetLiftTelemetry(w http.ResponseWriter, r *http.Request) {
	minutes := 30
	if mStr := r.URL.Query().Get("minutes"); mStr != "" {
		if m, err := strconv.Atoi(mStr); err == nil && m > 0 {
			minutes = m
		}
	}
	trend, err := db.GetTelemetryTrend(minutes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, trend)
}

// POST /api/v1/commands (PRD 9.2 & 9.3)
func (h *APIHandler) CreateCommand(w http.ResponseWriter, r *http.Request) {
	var req models.CreateCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "Gagal memproses payload request")
		return
	}

	// 1. Validasi Format QR: WH|<SKU>|<SERIAL>
	qrParts := strings.Split(req.QR, "|")
	if len(qrParts) != 3 || qrParts[0] != "WH" {
		writeError(w, http.StatusBadRequest, "QR_INVALID", "Format QR Code tidak valid (harus WH|<SKU>|<SERIAL>)")
		return
	}
	sku := qrParts[1]
	serial := qrParts[2]

	// 2. Cek Master SKU
	item, err := db.GetItemBySKU(sku)
	if err != nil || item == nil {
		writeError(w, http.StatusBadRequest, "ITEM_NOT_FOUND", fmt.Sprintf("SKU %s tidak terdaftar di master barang", sku))
		return
	}

	// 3. Cek Unit Fisik
	unit, err := db.GetUnitByQR(req.QR)
	if err != nil || unit == nil {
		// Auto-register unit if not exists
		newUnit, createErr := db.CreateBatchUnits(item.ID, models.GenerateUnitsRequest{
			Count: 1, StartSerial: 1, Prefix: serial,
		})
		if createErr == nil && len(newUnit) > 0 {
			unit = &newUnit[0]
		} else {
			writeError(w, http.StatusBadRequest, "ITEM_NOT_FOUND", "Unit fisik barang belum terdaftar")
			return
		}
	}

	// 4. Cek Status Unit berdasarkan Action
	if req.Action == "STORE" {
		if unit.Status == "STORED" {
			writeError(w, http.StatusBadRequest, "UNIT_ALREADY_STORED", "Unit ini sudah tersimpan di dalam rak")
			return
		}
	} else if req.Action == "RETRIEVE" {
		if unit.Status != "STORED" {
			writeError(w, http.StatusBadRequest, "UNIT_NOT_STORED", "Unit belum berstatus STORED di rak")
			return
		}
		if unit.Level != nil {
			req.TargetLevel = *unit.Level
		}
	} else {
		writeError(w, http.StatusBadRequest, "INVALID_ACTION", "Aksi tidak dikenal (harus STORE atau RETRIEVE)")
		return
	}

	// 5. Cek Kondisi Lift
	tele, status, _ := h.mqttSvc.GetCurrentState()
	if status == "offline" {
		writeError(w, http.StatusServiceUnavailable, "DEVICE_OFFLINE", "Perangkat ESP32 offline, tidak dapat menerima perintah")
		return
	}
	if !tele.Enabled {
		writeError(w, http.StatusBadRequest, "LIFT_DISABLED", "Lift sedang dinonaktifkan. Aktifkan driver terlebih dahulu")
		return
	}
	if tele.Mode != "AUTO" {
		writeError(w, http.StatusBadRequest, "MODE_MISMATCH", "Lift berada di mode MANUAL. Ubah ke mode AUTO untuk transaksi QR")
		return
	}
	if tele.Estop {
		writeError(w, http.StatusBadRequest, "ESTOP_ACTIVE", "Emergency Stop sedang aktif")
		return
	}
	if tele.LiftStatus == "FAULT" {
		writeError(w, http.StatusBadRequest, "LIFT_FAULT", "Lift sedang dalam status FAULT. Reset FAULT terlebih dahulu")
		return
	}
	if tele.FlowState != "IDLE" {
		writeError(w, http.StatusConflict, "LIFT_BUSY", "Lift sedang sibuk memproses perintah lain")
		return
	}

	// 6. Cek Kapasitas Level (untuk STORE)
	if req.Action == "STORE" {
		if req.TargetLevel < 1 || req.TargetLevel > 2 {
			req.TargetLevel = 2 // default upper rack
		}
		isFull, err := db.IsShelfFull(req.TargetLevel)
		if err == nil && isFull {
			writeError(w, http.StatusBadRequest, "LEVEL_FULL", fmt.Sprintf("Lantai rak %d sudah penuh", req.TargetLevel))
			return
		}
	}

	// 7. Ambil Setting Batas Max Load
	settings, _ := db.GetSettings()
	maxLoadG := 800.0
	if val, ok := settings["max_load_g"]; ok {
		if ml, err := strconv.ParseFloat(val, 64); err == nil {
			maxLoadG = ml
		}
	}

	// 8. Generate Command ID (cmd-YYYYMMDD-XXXX)
	cmdID := fmt.Sprintf("cmd-%s-%04d", time.Now().Format("20060102"), time.Now().UnixNano()%10000)

	// Update unit status to pending
	newUnitStatus := "PENDING_IN"
	if req.Action == "RETRIEVE" {
		newUnitStatus = "PENDING_OUT"
	}
	_ = db.UpdateUnitStatus(unit.ID, newUnitStatus, nil, nil)

	// Create command record in DB
	cmd := &models.Command{
		CommandID:   cmdID,
		UnitID:      &unit.ID,
		Action:      req.Action,
		Source:      "QR",
		TargetLevel: &req.TargetLevel,
		Status:      "PENDING",
		RequestedBy: req.RequestedBy,
	}
	if err := db.CreateCommand(cmd); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	// Publish to MQTT warehouse/lift01/command
	mqttPayload := models.MQTTCommandPayload{
		CommandID:       cmdID,
		Action:          req.Action,
		QR:              req.QR,
		TargetLevel:     req.TargetLevel,
		ExpectedWeightG: item.ExpectedWeightG,
		MaxLoadG:        maxLoadG,
		RequestedBy:     req.RequestedBy,
	}
	_ = h.mqttSvc.PublishCommand(mqttPayload)

	writeSuccess(w, map[string]interface{}{
		"command_id":   cmdID,
		"action":       req.Action,
		"target_level": req.TargetLevel,
		"status":       "PENDING",
		"sku":          item.SKU,
		"item_name":    item.Name,
		"serial":       serial,
	})
}

// GET /api/v1/commands/{id}
func (h *APIHandler) GetCommand(w http.ResponseWriter, r *http.Request) {
	cmdID := r.PathValue("id")
	if cmdID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Command ID required")
		return
	}
	cmd, err := db.GetCommandByID(cmdID)
	if err != nil || cmd == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Perintah tidak ditemukan")
		return
	}
	writeSuccess(w, cmd)
}

// POST /api/v1/lift/stop
func (h *APIHandler) StopLift(w http.ResponseWriter, r *http.Request) {
	cmdID := fmt.Sprintf("cmd-stop-%d", time.Now().Unix())
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		CommandID: cmdID,
		Action:    "STOP",
		Source:    "MANUAL",
	})
	_ = db.CreateAlarm(&models.Alarm{
		Type:      "E-STOP",
		Message:   "Perintah Emergency STOP ditekan dari Dashboard",
		CommandID: cmdID,
	})
	writeSuccess(w, map[string]string{"result": "STOPPED"})
}

// POST /api/v1/lift/reset
func (h *APIHandler) ResetLift(w http.ResponseWriter, r *http.Request) {
	tele, _, _ := h.mqttSvc.GetCurrentState()
	if tele.Estop {
		writeError(w, http.StatusBadRequest, "ESTOP_ACTIVE", "Lepas tombol E-stop fisik sebelum melakukan reset FAULT")
		return
	}
	cmdID := fmt.Sprintf("cmd-reset-%d", time.Now().Unix())
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		CommandID: cmdID,
		Action:    "RESET",
		Source:    "MANUAL",
	})
	writeSuccess(w, map[string]string{"result": "RESET_SENT"})
}

// POST /api/v1/lift/enable
func (h *APIHandler) EnableLift(w http.ResponseWriter, r *http.Request) {
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		Action: "ENABLE",
	})
	writeSuccess(w, map[string]string{"result": "ENABLED"})
}

// POST /api/v1/lift/disable
func (h *APIHandler) DisableLift(w http.ResponseWriter, r *http.Request) {
	tele, _, _ := h.mqttSvc.GetCurrentState()
	if tele.FlowState != "IDLE" {
		writeError(w, http.StatusBadRequest, "LIFT_BUSY", "Lift hanya bisa dinonaktifkan saat IDLE")
		return
	}
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		Action: "DISABLE",
	})
	writeSuccess(w, map[string]string{"result": "DISABLED"})
}

// POST /api/v1/lift/mode
func (h *APIHandler) SetLiftMode(w http.ResponseWriter, r *http.Request) {
	var req models.ModeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Mode = strings.ToUpper(req.Mode)
	if req.Mode != "AUTO" && req.Mode != "MANUAL" {
		writeError(w, http.StatusBadRequest, "INVALID_MODE", "Mode harus bernilai AUTO atau MANUAL")
		return
	}
	tele, _, _ := h.mqttSvc.GetCurrentState()
	if tele.FlowState != "IDLE" {
		writeError(w, http.StatusBadRequest, "LIFT_BUSY", "Mode hanya dapat diubah saat kondisi lift IDLE")
		return
	}
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		Action: "SET_MODE",
		Mode:   req.Mode,
	})
	writeSuccess(w, map[string]string{"mode": req.Mode})
}

// POST /api/v1/lift/move
func (h *APIHandler) ManualMove(w http.ResponseWriter, r *http.Request) {
	var req models.ManualMoveRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.TargetLevel != 1 && req.TargetLevel != 2 {
		writeError(w, http.StatusBadRequest, "INVALID_LEVEL", "Target lantai harus 1 atau 2")
		return
	}
	tele, status, _ := h.mqttSvc.GetCurrentState()
	if status == "offline" {
		writeError(w, http.StatusServiceUnavailable, "DEVICE_OFFLINE", "Perangkat ESP32 offline")
		return
	}
	if !tele.Enabled {
		writeError(w, http.StatusBadRequest, "LIFT_DISABLED", "Lift belum diaktifkan")
		return
	}
	if tele.Mode != "MANUAL" {
		writeError(w, http.StatusBadRequest, "MODE_MISMATCH", "Pindahkan mode ke MANUAL terlebih dahulu")
		return
	}
	if tele.FlowState != "IDLE" {
		writeError(w, http.StatusConflict, "LIFT_BUSY", "Lift sedang bergerak")
		return
	}

	cmdID := fmt.Sprintf("cmd-man-%d", time.Now().UnixNano()%100000)
	cmd := &models.Command{
		CommandID:   cmdID,
		Action:      "MOVE",
		Source:      "MANUAL",
		TargetLevel: &req.TargetLevel,
		Status:      "MOVING",
		RequestedBy: req.RequestedBy,
	}
	_ = db.CreateCommand(cmd)

	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		CommandID:   cmdID,
		Action:      "MOVE",
		TargetLevel: req.TargetLevel,
		Source:      "MANUAL",
		RequestedBy: req.RequestedBy,
	})
	writeSuccess(w, map[string]interface{}{"command_id": cmdID, "status": "MOVING"})
}

// POST /api/v1/lift/jog
func (h *APIHandler) JogLift(w http.ResponseWriter, r *http.Request) {
	var req models.JogRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Direction = strings.ToUpper(req.Direction)
	if req.Direction != "UP" && req.Direction != "DOWN" {
		writeError(w, http.StatusBadRequest, "INVALID_DIRECTION", "Direction harus UP atau DOWN")
		return
	}
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		Action:    "JOG",
		Direction: req.Direction,
		Source:    "MANUAL",
	})
	writeSuccess(w, map[string]string{"jog": req.Direction})
}

// POST /api/v1/lift/home
func (h *APIHandler) HomeLift(w http.ResponseWriter, r *http.Request) {
	cmdID := fmt.Sprintf("cmd-home-%d", time.Now().UnixNano()%100000)
	_ = h.mqttSvc.PublishCommand(models.MQTTCommandPayload{
		CommandID: cmdID,
		Action:    "HOME",
		Source:    "MANUAL",
	})
	writeSuccess(w, map[string]string{"command_id": cmdID, "status": "HOMING"})
}

// ---------------------- ITEMS & UNITS ----------------------

// GET /api/v1/items
func (h *APIHandler) GetItems(w http.ResponseWriter, r *http.Request) {
	items, err := db.GetItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, items)
}

// POST /api/v1/items
func (h *APIHandler) CreateItem(w http.ResponseWriter, r *http.Request) {
	var req models.CreateItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if req.SKU == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "REQUIRED_FIELDS", "SKU dan Nama Barang wajib diisi")
		return
	}
	if req.WeightTolerancePct <= 0 {
		req.WeightTolerancePct = 10.0
	}
	item, err := db.CreateItem(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SKU_EXISTS", "SKU sudah terdaftar atau terjadi kesalahan database")
		return
	}
	writeSuccess(w, item)
}

// PUT /api/v1/items/{id}
func (h *APIHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "ID tidak valid")
		return
	}
	var req models.CreateItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	item, err := db.UpdateItem(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, item)
}

// POST /api/v1/items/{id}/units (Generate Batch Units & QR)
func (h *APIHandler) GenerateUnits(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "ID tidak valid")
		return
	}
	var req models.GenerateUnitsRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100 {
		req.Count = 100
	}
	units, err := db.CreateBatchUnits(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, units)
}

// GET /api/v1/units/{qr}
func (h *APIHandler) GetUnitByQR(w http.ResponseWriter, r *http.Request) {
	qr := r.PathValue("qr")
	if qr == "" {
		writeError(w, http.StatusBadRequest, "QR_REQUIRED", "QR parameter wajib")
		return
	}
	unit, err := db.GetUnitByQR(qr)
	if err != nil || unit == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Unit barang dengan QR tersebut tidak ditemukan")
		return
	}
	writeSuccess(w, unit)
}

// ---------------------- STOCK & MOVEMENTS ----------------------

// GET /api/v1/stock/summary (PRD 6.4 Query 1)
func (h *APIHandler) GetStockSummary(w http.ResponseWriter, r *http.Request) {
	items, err := db.GetItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, items)
}

// GET /api/v1/stock/levels (PRD 6.4 Query 2)
func (h *APIHandler) GetStockLevels(w http.ResponseWriter, r *http.Request) {
	levels, err := db.GetShelfLevels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, levels)
}

// POST /api/v1/stock/adjust (PRD 7.4 FR-34)
func (h *APIHandler) AdjustStock(w http.ResponseWriter, r *http.Request) {
	var req models.StockAdjustRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "REASON_REQUIRED", "Alasan penyesuaian stok wajib diisi")
		return
	}
	unit, err := db.GetUnitByID(req.UnitID)
	if err != nil || unit == nil {
		writeError(w, http.StatusNotFound, "UNIT_NOT_FOUND", "Unit tidak ditemukan")
		return
	}

	_ = db.UpdateUnitStatus(unit.ID, req.NewStatus, req.TargetLevel, nil)
	_ = db.RecordMovement(&models.StockMovement{
		UnitID:    &unit.ID,
		CommandID: "MANUAL-ADJUST",
		Type:      "ADJUST",
		Level:     req.TargetLevel,
		Status:    "SUCCESS",
		Note:      fmt.Sprintf("[%s] %s", req.RequestedBy, req.Reason),
	})

	writeSuccess(w, map[string]string{"result": "ADJUSTED"})
}

// GET /api/v1/movements (PRD 6.4 Query 3 & FR-40)
func (h *APIHandler) GetMovements(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil && o >= 0 {
			offset = o
		}
	}

	sku := r.URL.Query().Get("sku")
	mType := r.URL.Query().Get("type")
	qrSearch := r.URL.Query().Get("search")
	var levelPtr *int
	if lvlStr := r.URL.Query().Get("level"); lvlStr != "" {
		if lvl, err := strconv.Atoi(lvlStr); err == nil {
			levelPtr = &lvl
		}
	}

	list, total, err := db.GetMovements(limit, offset, sku, levelPtr, mType, qrSearch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"items":  list,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ---------------------- ALARMS & SETTINGS ----------------------

// GET /api/v1/alarms (PRD 7.5 FR-41)
func (h *APIHandler) GetAlarms(w http.ResponseWriter, r *http.Request) {
	unack := r.URL.Query().Get("unack") == "true"
	alarms, err := db.GetAlarms(unack)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, alarms)
}

// PATCH /api/v1/alarms/{id}/ack
func (h *APIHandler) AckAlarm(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "ID tidak valid")
		return
	}
	var req struct {
		User string `json:"user"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.User == "" {
		req.User = "Supervisor"
	}
	if err := db.AckAlarm(id, req.User); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, map[string]string{"result": "ACKNOWLEDGED"})
}

// GET /api/v1/settings
func (h *APIHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := db.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	writeSuccess(w, s)
}

// PUT /api/v1/settings
func (h *APIHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	for k, v := range body {
		_ = db.UpdateSetting(k, v)
	}
	writeSuccess(w, map[string]string{"result": "UPDATED"})
}
