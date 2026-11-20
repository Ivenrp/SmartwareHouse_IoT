-- Smart Warehouse: skema database untuk backend-go
-- Cara pakai: jalankan seluruh file ini di MySQL/phpMyAdmin (tab SQL).

CREATE DATABASE IF NOT EXISTS smart_warehouse CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE smart_warehouse;

CREATE TABLE IF NOT EXISTS items (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  sku VARCHAR(50) NOT NULL UNIQUE,
  name VARCHAR(150) NOT NULL,
  category VARCHAR(80) NULL,
  expected_weight_g DOUBLE NOT NULL DEFAULT 0,
  weight_tolerance_pct DOUBLE NOT NULL DEFAULT 10,
  min_stock INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS item_units (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  item_id BIGINT NOT NULL,
  serial VARCHAR(20) NOT NULL,
  qr_code VARCHAR(120) NOT NULL UNIQUE,
  status VARCHAR(20) NOT NULL DEFAULT 'REGISTERED',
  level INT NULL,
  weight_in_g DOUBLE NULL,
  stored_at DATETIME NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  INDEX idx_units_status_level (status, level),
  CONSTRAINT fk_units_item FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS shelf_levels (
  level INT PRIMARY KEY,
  capacity INT NOT NULL,
  ref_distance_cm DOUBLE NOT NULL,
  is_input_area TINYINT(1) NOT NULL DEFAULT 0
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS commands (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  command_id VARCHAR(60) NOT NULL UNIQUE,
  unit_id BIGINT NULL,
  action VARCHAR(20) NOT NULL,
  source VARCHAR(20) NOT NULL,
  target_level INT NULL,
  status VARCHAR(20) NOT NULL,
  fault_reason TEXT NULL,
  requested_by VARCHAR(80) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  completed_at DATETIME NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS stock_movements (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  unit_id BIGINT NULL,
  command_id VARCHAR(60) NOT NULL DEFAULT '',
  type VARCHAR(10) NOT NULL,
  level INT NULL,
  weight_g DOUBLE NULL,
  status VARCHAR(20) NOT NULL,
  note TEXT NULL,
  created_at DATETIME NOT NULL,
  INDEX idx_mv_created (created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS telemetry (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  device_id VARCHAR(40) NOT NULL,
  current_level INT NOT NULL,
  distance_cm DOUBLE NOT NULL,
  weight_g DOUBLE NOT NULL,
  estop TINYINT(1) NOT NULL DEFAULT 0,
  flow_state VARCHAR(20) NOT NULL,
  lift_status VARCHAR(20) NOT NULL,
  travel_time_sec DOUBLE NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  mode VARCHAR(10) NOT NULL DEFAULT 'AUTO',
  command_id VARCHAR(60) NULL,
  recorded_at DATETIME NOT NULL,
  INDEX idx_tele_dev_time (device_id, recorded_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS alarms (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  type VARCHAR(30) NOT NULL,
  message TEXT NOT NULL,
  command_id VARCHAR(60) NULL,
  is_acknowledged TINYINT(1) NOT NULL DEFAULT 0,
  acknowledged_by VARCHAR(80) NULL,
  created_at DATETIME NOT NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS settings (
  `key` VARCHAR(60) PRIMARY KEY,
  `value` VARCHAR(255) NOT NULL,
  description VARCHAR(255) NULL,
  updated_at DATETIME NULL
) ENGINE=InnoDB;

-- ---------- Data awal ----------
-- Jarak referensi sama dengan simulator: Lantai 1 = 5.0 cm, Lantai 2 = 18.5 cm
INSERT IGNORE INTO shelf_levels (level, capacity, ref_distance_cm, is_input_area) VALUES
  (1, 4, 5.0, 1),
  (2, 4, 18.5, 0);

INSERT IGNORE INTO settings (`key`, `value`, description, updated_at) VALUES
  ('max_load_g', '800', 'Batas beban maksimum platform (gram)', NOW());

-- Barang contoh + 4 unit supaya bisa langsung diuji (kode QR: WH|SKU-001|000001 dst.)
INSERT IGNORE INTO items (sku, name, category, expected_weight_g, weight_tolerance_pct, min_stock, created_at, updated_at)
VALUES ('SKU-001', 'Barang Contoh', 'Uji coba', 100, 10, 1, NOW(), NOW());

INSERT IGNORE INTO item_units (item_id, serial, qr_code, status, created_at, updated_at)
SELECT i.id, LPAD(n.n, 6, '0'), CONCAT('WH|SKU-001|', LPAD(n.n, 6, '0')), 'REGISTERED', NOW(), NOW()
FROM items i
JOIN (SELECT 1 AS n UNION SELECT 2 UNION SELECT 3 UNION SELECT 4) n
WHERE i.sku = 'SKU-001';
