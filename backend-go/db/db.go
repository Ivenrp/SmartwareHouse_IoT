package db

import (
	"database/sql"
	"fmt"
	"log"
	"smart-warehouse-backend/config"
	"smart-warehouse-backend/models"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=Local",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(10)
	DB.SetConnMaxLifetime(5 * time.Minute)

	if err = DB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("[Database] Connected successfully to MySQL:", cfg.DBName)
	return DB, nil
}

// ---------------------- ITEMS (MASTER BARANG) ----------------------

func GetItems() ([]models.Item, error) {
	query := `
		SELECT i.id, i.sku, i.name, COALESCE(i.category, ''), i.expected_weight_g, 
		       i.weight_tolerance_pct, i.min_stock, i.created_at, i.updated_at,
		       COUNT(u.id) AS stok
		FROM items i
		LEFT JOIN item_units u ON u.item_id = i.id AND u.status = 'STORED'
		GROUP BY i.id
		ORDER BY i.sku ASC`

	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.Item
	for rows.Next() {
		var it models.Item
		if err := rows.Scan(&it.ID, &it.SKU, &it.Name, &it.Category, &it.ExpectedWeightG,
			&it.WeightTolerancePct, &it.MinStock, &it.CreatedAt, &it.UpdatedAt, &it.CurrentStock); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func GetItemBySKU(sku string) (*models.Item, error) {
	query := `
		SELECT id, sku, name, COALESCE(category, ''), expected_weight_g, 
		       weight_tolerance_pct, min_stock, created_at, updated_at
		FROM items WHERE sku = ? LIMIT 1`

	var it models.Item
	err := DB.QueryRow(query, sku).Scan(&it.ID, &it.SKU, &it.Name, &it.Category,
		&it.ExpectedWeightG, &it.WeightTolerancePct, &it.MinStock, &it.CreatedAt, &it.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func CreateItem(req models.CreateItemRequest) (*models.Item, error) {
	query := `INSERT INTO items (sku, name, category, expected_weight_g, weight_tolerance_pct, min_stock, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`
	res, err := DB.Exec(query, req.SKU, req.Name, req.Category, req.ExpectedWeightG, req.WeightTolerancePct, req.MinStock)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetItemByID(id)
}

func GetItemByID(id int64) (*models.Item, error) {
	query := `
		SELECT i.id, i.sku, i.name, COALESCE(i.category, ''), i.expected_weight_g, 
		       i.weight_tolerance_pct, i.min_stock, i.created_at, i.updated_at,
		       COUNT(u.id) AS stok
		FROM items i
		LEFT JOIN item_units u ON u.item_id = i.id AND u.status = 'STORED'
		WHERE i.id = ?
		GROUP BY i.id`

	var it models.Item
	err := DB.QueryRow(query, id).Scan(&it.ID, &it.SKU, &it.Name, &it.Category, &it.ExpectedWeightG,
		&it.WeightTolerancePct, &it.MinStock, &it.CreatedAt, &it.UpdatedAt, &it.CurrentStock)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func UpdateItem(id int64, req models.CreateItemRequest) (*models.Item, error) {
	query := `UPDATE items SET sku=?, name=?, category=?, expected_weight_g=?, weight_tolerance_pct=?, min_stock=?, updated_at=NOW() WHERE id=?`
	_, err := DB.Exec(query, req.SKU, req.Name, req.Category, req.ExpectedWeightG, req.WeightTolerancePct, req.MinStock, id)
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

// ---------------------- ITEM UNITS & QR ----------------------

func GetUnitByQR(qr string) (*models.ItemUnit, error) {
	query := `
		SELECT u.id, u.item_id, i.sku, i.name, COALESCE(i.category, ''), u.serial, u.qr_code, 
		       u.status, u.level, u.weight_in_g, u.stored_at, u.created_at, u.updated_at
		FROM item_units u
		JOIN items i ON i.id = u.item_id
		WHERE u.qr_code = ? LIMIT 1`

	var u models.ItemUnit
	err := DB.QueryRow(query, qr).Scan(&u.ID, &u.ItemID, &u.SKU, &u.ItemName, &u.Category,
		&u.Serial, &u.QRCode, &u.Status, &u.Level, &u.WeightInG, &u.StoredAt, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func GetUnitByID(id int64) (*models.ItemUnit, error) {
	query := `
		SELECT u.id, u.item_id, i.sku, i.name, COALESCE(i.category, ''), u.serial, u.qr_code, 
		       u.status, u.level, u.weight_in_g, u.stored_at, u.created_at, u.updated_at
		FROM item_units u
		JOIN items i ON i.id = u.item_id
		WHERE u.id = ? LIMIT 1`

	var u models.ItemUnit
	err := DB.QueryRow(query, id).Scan(&u.ID, &u.ItemID, &u.SKU, &u.ItemName, &u.Category,
		&u.Serial, &u.QRCode, &u.Status, &u.Level, &u.WeightInG, &u.StoredAt, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func CreateBatchUnits(itemID int64, req models.GenerateUnitsRequest) ([]models.ItemUnit, error) {
	item, err := GetItemByID(itemID)
	if err != nil || item == nil {
		return nil, fmt.Errorf("item not found")
	}

	start := req.StartSerial
	if start <= 0 {
		var maxSerial sql.NullString
		_ = DB.QueryRow("SELECT MAX(serial) FROM item_units WHERE item_id = ?", itemID).Scan(&maxSerial)
		start = 1
	}

	var createdUnits []models.ItemUnit
	for i := 0; i < req.Count; i++ {
		serialNum := start + i
		serialStr := fmt.Sprintf("%06d", serialNum)
		qrCode := fmt.Sprintf("WH|%s|%s", item.SKU, serialStr)

		query := `INSERT INTO item_units (item_id, serial, qr_code, status, created_at, updated_at) 
		          VALUES (?, ?, ?, 'REGISTERED', NOW(), NOW())`
		res, err := DB.Exec(query, itemID, serialStr, qrCode)
		if err != nil {
			continue // skip duplicate serial if any
		}
		uid, _ := res.LastInsertId()
		unit, _ := GetUnitByID(uid)
		if unit != nil {
			createdUnits = append(createdUnits, *unit)
		}
	}
	return createdUnits, nil
}

func UpdateUnitStatus(id int64, status string, level *int, weight *float64) error {
	var query string
	var err error
	if status == "STORED" {
		query = `UPDATE item_units SET status=?, level=?, weight_in_g=?, stored_at=NOW(), updated_at=NOW() WHERE id=?`
		_, err = DB.Exec(query, status, level, weight, id)
	} else if status == "OUT" {
		query = `UPDATE item_units SET status=?, level=NULL, updated_at=NOW() WHERE id=?`
		_, err = DB.Exec(query, status, id)
	} else {
		query = `UPDATE item_units SET status=?, updated_at=NOW() WHERE id=?`
		_, err = DB.Exec(query, status, id)
	}
	return err
}

// ---------------------- SHELF LEVELS & CAPACITY ----------------------

func GetShelfLevels() ([]models.ShelfLevel, error) {
	query := `
		SELECT s.level, s.capacity, s.ref_distance_cm, s.is_input_area, 
		       COUNT(u.id) AS terisi,
		       s.capacity - COUNT(u.id) AS sisa
		FROM shelf_levels s
		LEFT JOIN item_units u ON u.level = s.level AND u.status = 'STORED'
		GROUP BY s.level
		ORDER BY s.level ASC`

	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var levels []models.ShelfLevel
	for rows.Next() {
		var s models.ShelfLevel
		if err := rows.Scan(&s.Level, &s.Capacity, &s.RefDistanceCm, &s.IsInputArea, &s.OccupiedCount, &s.AvailableSlot); err != nil {
			return nil, err
		}
		levels = append(levels, s)
	}
	return levels, nil
}

func IsShelfFull(level int) (bool, error) {
	var capacity int
	var occupied int
	err := DB.QueryRow("SELECT capacity FROM shelf_levels WHERE level = ?", level).Scan(&capacity)
	if err != nil {
		return false, err
	}
	err = DB.QueryRow("SELECT COUNT(*) FROM item_units WHERE level = ? AND status = 'STORED'", level).Scan(&occupied)
	if err != nil {
		return false, err
	}
	return occupied >= capacity, nil
}

// ---------------------- COMMANDS ----------------------

func CreateCommand(cmd *models.Command) error {
	query := `
		INSERT INTO commands (command_id, unit_id, action, source, target_level, status, fault_reason, requested_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW())`

	res, err := DB.Exec(query, cmd.CommandID, cmd.UnitID, cmd.Action, cmd.Source, cmd.TargetLevel, cmd.Status, cmd.FaultReason, cmd.RequestedBy)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	cmd.ID = id
	return nil
}

func UpdateCommandStatus(commandID string, status string, faultReason *string) error {
	var query string
	if status == "COMPLETE" || status == "FAILED" {
		query = `UPDATE commands SET status=?, fault_reason=?, completed_at=NOW() WHERE command_id=?`
	} else {
		query = `UPDATE commands SET status=?, fault_reason=? WHERE command_id=?`
	}
	_, err := DB.Exec(query, status, faultReason, commandID)
	return err
}

func GetCommandByID(commandID string) (*models.Command, error) {
	query := `
		SELECT c.id, c.command_id, c.unit_id, COALESCE(u.qr_code, ''), c.action, c.source, 
		       c.target_level, c.status, c.fault_reason, c.requested_by, c.created_at, c.completed_at
		FROM commands c
		LEFT JOIN item_units u ON u.id = c.unit_id
		WHERE c.command_id = ? LIMIT 1`

	var cmd models.Command
	err := DB.QueryRow(query, commandID).Scan(
		&cmd.ID, &cmd.CommandID, &cmd.UnitID, &cmd.QRCode, &cmd.Action, &cmd.Source,
		&cmd.TargetLevel, &cmd.Status, &cmd.FaultReason, &cmd.RequestedBy, &cmd.CreatedAt, &cmd.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}

// ---------------------- STOCK MOVEMENTS (LEDGER) ----------------------

func RecordMovement(movement *models.StockMovement) error {
	query := `
		INSERT INTO stock_movements (unit_id, command_id, type, level, weight_g, status, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW())`

	res, err := DB.Exec(query, movement.UnitID, movement.CommandID, movement.Type, movement.Level, movement.WeightG, movement.Status, movement.Note)
	if err != nil {
		return err
	}
	movement.ID, _ = res.LastInsertId()
	return nil
}

func GetMovements(limit int, offset int, sku string, level *int, mType string, qrSearch string) ([]models.StockMovement, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}

	if sku != "" {
		where = append(where, "i.sku = ?")
		args = append(args, sku)
	}
	if level != nil {
		where = append(where, "m.level = ?")
		args = append(args, *level)
	}
	if mType != "" {
		where = append(where, "m.type = ?")
		args = append(args, mType)
	}
	if qrSearch != "" {
		where = append(where, "(u.qr_code LIKE ? OR u.serial LIKE ?)")
		args = append(args, "%"+qrSearch+"%", "%"+qrSearch+"%")
	}

	whereClause := strings.Join(where, " AND ")

	countQuery := fmt.Sprintf(`
		SELECT COUNT(m.id)
		FROM stock_movements m
		LEFT JOIN item_units u ON u.id = m.unit_id
		LEFT JOIN items i ON i.id = u.item_id
		WHERE %s`, whereClause)

	var total int
	err := DB.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT m.id, m.unit_id, COALESCE(i.sku, ''), COALESCE(u.serial, ''), m.command_id, 
		       m.type, m.level, m.weight_g, m.status, COALESCE(m.note, ''), m.created_at
		FROM stock_movements m
		LEFT JOIN item_units u ON u.id = m.unit_id
		LEFT JOIN items i ON i.id = u.item_id
		WHERE %s
		ORDER BY m.created_at DESC
		LIMIT ? OFFSET ?`, whereClause)

	args = append(args, limit, offset)
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var movements []models.StockMovement
	for rows.Next() {
		var m models.StockMovement
		if err := rows.Scan(&m.ID, &m.UnitID, &m.SKU, &m.Serial, &m.CommandID,
			&m.Type, &m.Level, &m.WeightG, &m.Status, &m.Note, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		movements = append(movements, m)
	}
	return movements, total, nil
}

// ---------------------- TELEMETRY & ALARMS ----------------------

func SaveTelemetry(t *models.Telemetry) error {
	query := `
		INSERT INTO telemetry (device_id, current_level, distance_cm, weight_g, estop, flow_state, lift_status, travel_time_sec, enabled, mode, command_id, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())`

	_, err := DB.Exec(query, t.DeviceID, t.CurrentLevel, t.DistanceCm, t.WeightG, t.Estop, t.FlowState, t.LiftStatus, t.TravelTimeSec, t.Enabled, t.Mode, t.CommandID)
	return err
}

func GetLatestTelemetry(deviceID string) (*models.Telemetry, error) {
	query := `
		SELECT id, device_id, current_level, distance_cm, weight_g, estop, flow_state, lift_status, travel_time_sec, enabled, mode, COALESCE(command_id, ''), recorded_at
		FROM telemetry
		WHERE device_id = ?
		ORDER BY recorded_at DESC, id DESC
		LIMIT 1`

	var t models.Telemetry
	err := DB.QueryRow(query, deviceID).Scan(
		&t.ID, &t.DeviceID, &t.CurrentLevel, &t.DistanceCm, &t.WeightG, &t.Estop,
		&t.FlowState, &t.LiftStatus, &t.TravelTimeSec, &t.Enabled, &t.Mode, &t.CommandID, &t.RecordedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func GetTelemetryTrend(minutes int) ([]models.Telemetry, error) {
	if minutes <= 0 {
		minutes = 30
	}
	query := `
		SELECT id, device_id, current_level, distance_cm, weight_g, estop, flow_state, lift_status, travel_time_sec, enabled, mode, COALESCE(command_id, ''), recorded_at
		FROM telemetry
		WHERE recorded_at >= NOW() - INTERVAL ? MINUTE
		ORDER BY recorded_at ASC`

	rows, err := DB.Query(query, minutes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Telemetry
	for rows.Next() {
		var t models.Telemetry
		if err := rows.Scan(&t.ID, &t.DeviceID, &t.CurrentLevel, &t.DistanceCm, &t.WeightG, &t.Estop,
			&t.FlowState, &t.LiftStatus, &t.TravelTimeSec, &t.Enabled, &t.Mode, &t.CommandID, &t.RecordedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, nil
}

func CreateAlarm(a *models.Alarm) error {
	query := `INSERT INTO alarms (type, message, command_id, is_acknowledged, created_at) VALUES (?, ?, ?, false, NOW())`
	res, err := DB.Exec(query, a.Type, a.Message, a.CommandID)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

func GetAlarms(unackOnly bool) ([]models.Alarm, error) {
	query := `
		SELECT id, type, message, COALESCE(command_id, ''), is_acknowledged, acknowledged_by, created_at
		FROM alarms`
	if unackOnly {
		query += " WHERE is_acknowledged = false"
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alarms []models.Alarm
	for rows.Next() {
		var a models.Alarm
		if err := rows.Scan(&a.ID, &a.Type, &a.Message, &a.CommandID, &a.IsAcknowledged, &a.AcknowledgedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		alarms = append(alarms, a)
	}
	return alarms, nil
}

func AckAlarm(id int64, byUser string) error {
	query := `UPDATE alarms SET is_acknowledged = true, acknowledged_by = ? WHERE id = ?`
	_, err := DB.Exec(query, byUser, id)
	return err
}

// ---------------------- SETTINGS ----------------------

func GetSettings() (map[string]string, error) {
	rows, err := DB.Query("SELECT `key`, `value` FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			settings[k] = v
		}
	}
	return settings, nil
}

func UpdateSetting(key, val string) error {
	query := `UPDATE settings SET value = ?, updated_at = NOW() WHERE key = ?`
	_, err := DB.Exec(query, val, key)
	return err
}
