package models

import "time"

// JSON Response Wrapper as specified in PRD Section 9.3
type APIResponse struct {
	Data  interface{} `json:"data"`
	Error *APIError   `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Item struct {
	ID                 int64     `json:"id"`
	SKU                string    `json:"sku"`
	Name               string    `json:"name"`
	Category           string    `json:"category"`
	ExpectedWeightG    float64   `json:"expected_weight_g"`
	WeightTolerancePct float64   `json:"weight_tolerance_pct"`
	MinStock           int       `json:"min_stock"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CurrentStock       int       `json:"current_stock,omitempty"`
}

type ItemUnit struct {
	ID        int64      `json:"id"`
	ItemID    int64      `json:"item_id"`
	SKU       string     `json:"sku,omitempty"`
	ItemName  string     `json:"item_name,omitempty"`
	Category  string     `json:"category,omitempty"`
	Serial    string     `json:"serial"`
	QRCode    string     `json:"qr_code"`
	Status    string     `json:"status"` // REGISTERED, PENDING_IN, STORED, PENDING_OUT, OUT, FAILED
	Level     *int       `json:"level"`
	WeightInG *float64   `json:"weight_in_g"`
	StoredAt  *time.Time `json:"stored_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type ShelfLevel struct {
	Level         int     `json:"level"`
	Capacity      int     `json:"capacity"`
	RefDistanceCm float64 `json:"ref_distance_cm"`
	IsInputArea   bool    `json:"is_input_area"`
	OccupiedCount int     `json:"occupied_count"`
	AvailableSlot int     `json:"available_slot"`
}

type Command struct {
	ID          int64      `json:"id"`
	CommandID   string     `json:"command_id"`
	UnitID      *int64     `json:"unit_id"`
	QRCode      string     `json:"qr_code,omitempty"`
	Action      string     `json:"action"` // STORE, RETRIEVE, ENABLE, DISABLE, SET_MODE, MOVE, JOG, HOME, STOP, RESET
	Source      string     `json:"source"` // QR, MANUAL
	TargetLevel *int       `json:"target_level"`
	Status      string     `json:"status"` // PENDING, MOVING, VERIFYING, COMPLETE, FAILED
	FaultReason *string    `json:"fault_reason"`
	RequestedBy string     `json:"requested_by"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type StockMovement struct {
	ID        int64     `json:"id"`
	UnitID    *int64    `json:"unit_id"`
	SKU       string    `json:"sku,omitempty"`
	Serial    string    `json:"serial,omitempty"`
	CommandID string    `json:"command_id"`
	Type      string    `json:"type"` // IN, OUT, ADJUST
	Level     *int      `json:"level"`
	WeightG   *float64  `json:"weight_g"`
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type Telemetry struct {
	ID            int64     `json:"id,omitempty"`
	DeviceID      string    `json:"device_id"`
	CurrentLevel  int       `json:"current_level"`
	DistanceCm    float64   `json:"distance_cm"`
	WeightG       float64   `json:"weight_g"`
	Estop         bool      `json:"estop"`
	FlowState     string    `json:"flow_state"` // IDLE, VALIDASI, MOVING, VERIFY, COMPLETE
	LiftStatus    string    `json:"lift_status"` // STOPPED, UP, DOWN, BRAKING, FAULT
	TravelTimeSec float64   `json:"travel_time_sec"`
	Enabled       bool      `json:"enabled"`
	Mode          string    `json:"mode"` // AUTO, MANUAL
	CommandID     string    `json:"command_id,omitempty"`
	Timestamp     string    `json:"timestamp,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type Alarm struct {
	ID             int64      `json:"id"`
	Type           string     `json:"type"` // E-STOP, OVERLOAD, TRAVEL_TIMEOUT, WEIGHT_MISMATCH, DEVICE_OFFLINE, LIFT_FAULT
	Message        string     `json:"message"`
	CommandID      string     `json:"command_id"`
	IsAcknowledged bool       `json:"is_acknowledged"`
	AcknowledgedBy *string    `json:"acknowledged_by"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Setting struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// Request Bodies
type CreateCommandRequest struct {
	QR          string `json:"qr"`
	Action      string `json:"action"` // STORE or RETRIEVE
	TargetLevel int    `json:"target_level"`
	RequestedBy string `json:"requested_by"`
}

type ManualMoveRequest struct {
	TargetLevel int    `json:"target_level"`
	RequestedBy string `json:"requested_by"`
}

type JogRequest struct {
	Direction   string `json:"direction"` // UP or DOWN
	RequestedBy string `json:"requested_by"`
}

type ModeRequest struct {
	Mode        string `json:"mode"` // AUTO or MANUAL
	RequestedBy string `json:"requested_by"`
}

type StockAdjustRequest struct {
	UnitID      int64   `json:"unit_id"`
	TargetLevel *int    `json:"target_level"`
	NewStatus   string  `json:"new_status"` // STORED, OUT
	Reason      string  `json:"reason"`
	RequestedBy string  `json:"requested_by"`
}

type CreateItemRequest struct {
	SKU                string  `json:"sku"`
	Name               string  `json:"name"`
	Category           string  `json:"category"`
	ExpectedWeightG    float64 `json:"expected_weight_g"`
	WeightTolerancePct float64 `json:"weight_tolerance_pct"`
	MinStock           int     `json:"min_stock"`
}

type GenerateUnitsRequest struct {
	Count       int    `json:"count"`
	StartSerial int    `json:"start_serial"`
	Prefix      string `json:"prefix"`
}

// MQTT Payloads as defined in PRD Section 9.2
type MQTTCommandPayload struct {
	CommandID       string  `json:"command_id"`
	Action          string  `json:"action"` // STORE, RETRIEVE, MOVE, JOG, HOME, STOP, RESET, ENABLE, DISABLE, SET_MODE
	QR              string  `json:"qr,omitempty"`
	TargetLevel     int     `json:"target_level,omitempty"`
	ExpectedWeightG float64 `json:"expected_weight_g,omitempty"`
	MaxLoadG        float64 `json:"max_load_g,omitempty"`
	Source          string  `json:"source,omitempty"`
	Direction       string  `json:"direction,omitempty"`
	Mode            string  `json:"mode,omitempty"`
	RequestedBy     string  `json:"requested_by,omitempty"`
	Timestamp       string  `json:"timestamp"`
}

type MQTTTelemetryPayload struct {
	DeviceID      string  `json:"device_id"`
	CurrentLevel  int     `json:"current_level"`
	DistanceCm    float64 `json:"distance_cm"`
	WeightG       float64 `json:"weight_g"`
	Estop         bool    `json:"estop"`
	FlowState     string  `json:"flow_state"`
	LiftStatus    string  `json:"lift_status"`
	TravelTimeSec float64 `json:"travel_time_sec"`
	CommandID     string  `json:"command_id,omitempty"`
	Enabled       bool    `json:"enabled"`
	Mode          string  `json:"mode"`
	Timestamp     string  `json:"timestamp"`
}

type MQTTEventPayload struct {
	CommandID    string  `json:"command_id"`
	Result       string  `json:"result"` // COMPLETE or FAILED
	FinalLevel   int     `json:"final_level"`
	FinalWeightG float64 `json:"final_weight_g"`
	FaultReason  *string `json:"fault_reason"`
	Timestamp    string  `json:"timestamp,omitempty"`
}
