#include <WiFi.h>
#include <PubSubClient.h>
#include <ArduinoJson.h>
#include <AccelStepper.h>
#include <HX711.h>
#include <Preferences.h>

// ============================================================================
// 1. KONFIGURASI JARINGAN & MQTT
// ============================================================================
const char* WIFI_SSID     = "zulfan";
const char* WIFI_PASSWORD = "12345678";

const char* MQTT_BROKER   = "broker.hivemq.com"; // Ganti dengan IP Mosquitto / HiveMQ lokal Anda
const int   MQTT_PORT     = 1883;
const char* DEVICE_ID     = "SMART-WH-LIFT-01";

// Topik MQTT sesuai PRD v1.1
const char* TOPIC_TELEMETRY = "warehouse/lift-01/telemetry";
const char* TOPIC_COMMAND   = "warehouse/lift-01/command";
const char* TOPIC_ACK       = "warehouse/lift-01/ack";
const char* TOPIC_STATUS    = "warehouse/lift-01/status";

// ============================================================================
// 2. DEFINISI PIN PERANGKAT KERAS
// ============================================================================
// Stepper Motor NEMA 17 (Driver A4988 / DRV8825 / TMC2209)
#define PIN_STEP    14
#define PIN_DIR     12
#define PIN_ENABLE  27

// Sensor Ultrasonik 1: Deteksi keberadaan barang di platform Lantai 1
#define TRIG_U1     5
#define ECHO_U1     18

// Sensor Ultrasonik 2: Deteksi ketepatan berhenti lift (dipasang di atas rangka)
#define TRIG_U2     21
#define ECHO_U2     19

// Sensor Ultrasonik 3: Deteksi sisa ruang penyimpanan di Lantai 2
#define TRIG_U3     22
#define ECHO_U3     23

// Modul Load Cell HX711
// CATATAN: Dipindah dari GPIO1/3 (RX0/TX0) ke GPIO4/15 agar tidak
// bentrok dengan jalur serial USB saat upload sketch.
#define PIN_HX_DOUT 4
#define PIN_HX_SCK  15

// LED Indikator (masing-masing seri dengan resistor 220-330 ohm ke GND)
// CATATAN: GPIO34/35 hanya input di ESP32, jadi LED dipasang di GPIO25/26.
#define PIN_LED_RED    25  // Berkedip = emergency stop / error halt
#define PIN_LED_GREEN  26  // Menyala  = lift sedang bergerak

// ============================================================================
// 3. PARAMETER KALIBRASI & BATAS MEKANIS (SESUAIKAN DENGAN FISIK ALAT)
// ============================================================================
// Parameter Stepper Motor
const float SPEED_NORMAL        = 1000.0; // Steps per detik saat jelajah
const float SPEED_APPROACH      = 350.0;  // Steps per detik saat mendekati titik berhenti
const float ACCEL_NORMAL        = 600.0;  // Akselerasi halus agar barang tidak bergeser
const long  MAX_TRAVEL_STEPS    = 15000;  // Batas langkah maksimal pengaman (failsafe)

// Parameter Ultrasonik 1 (Deteksi Barang Lantai 1)
const float THRESHOLD_ITEM_CM   = 12.0;   // Jarak < 12 cm berarti ada barang di platform
const unsigned long STABLE_ITEM_MS = 2000; // Barang harus stabil 2 detik sebelum diproses

// Parameter Ultrasonik 2 (Posisi Lantai - Sensor di atas menghadap ke platform)
const float DIST_U2_FLOOR_2_CM  = 6.0;    // Jarak platform saat tepat di Lantai 2
const float DIST_U2_FLOOR_1_CM  = 45.0;   // Jarak platform saat tepat di Lantai 1
const float ALIGN_TOLERANCE_CM  = 0.3;    // Toleransi ketepatan berhenti (+/- 3 mm)
const float SLOWDOWN_ZONE_CM    = 5.0;    // Mulai melambat 5 cm sebelum titik berhenti

// Parameter Ultrasonik 3 & Stok Lantai 2 (hitung barang dari sisa jarak, cm)
//   jarak > U3_EMPTY_CM            -> ruang kosong
//   jarak mengecil & stabil 5 detik -> barang +1
//   jarak <= U3_FULL_CM (terdekat)  -> ruang penuh
const float U3_EMPTY_CM         = 80.0;   // Jarak > 80 cm = rak Lantai 2 kosong
const float U3_FULL_CM          = 10.0;   // Jarak paling dekat = rak Lantai 2 penuh
const int   MAX_CAPACITY_SLOTS  = 4;      // Kapasitas maksimal unit di Lantai 2
const float SLOT_WIDTH_CM       = (U3_EMPTY_CM - U3_FULL_CM) / MAX_CAPACITY_SLOTS; // Jarak per 1 slot
const float U3_CHANGE_MIN_CM    = 4.0;    // Perubahan jarak minimal agar dianggap barang masuk/keluar
const float U3_STABLE_BAND_CM   = 2.0;    // Jarak dianggap "stabil" jika bergeser < 2 cm
const unsigned long U3_CONFIRM_MS = 5000; // Perubahan harus bertahan 5 detik

// Parameter Load Cell
float LOADCELL_CALIBRATION      = 420.5;  // Ubah sesuai hasil kalibrasi load cell Anda
const float MAX_WEIGHT_GRAMS    = 3000.0; // Batas aman beban NEMA 17 (3 kg)
const float EMPTY_WEIGHT_GRAMS  = 25.0;   // Ambang batas platform dianggap kosong

// Parameter LED
const unsigned long LED_BLINK_INTERVAL_MS = 300; // Kecepatan kedip LED merah

// ============================================================================
// 4. OBJEK & VARIABEL GLOBAL STATE MACHINE
// ============================================================================
WiFiClient espClient;
PubSubClient mqttClient(espClient);
AccelStepper stepper(AccelStepper::DRIVER, PIN_STEP, PIN_DIR);
HX711 scale;
Preferences preferences;

enum SystemState {
  STATE_IDLE_FLOOR_1,
  STATE_WEIGHING_AND_CHECK,
  STATE_LIFT_ASCENDING,
  STATE_UNLOADING_FLOOR_2,
  STATE_LIFT_DESCENDING,
  STATE_ERROR_OVERWEIGHT,
  STATE_WAIT_FLOOR_2_SPACE,
  STATE_EMERGENCY_HALT,
  STATE_MANUAL_JOG  // Mode tes manual: jog motor langkah kecil via dashboard
};

SystemState currentState = STATE_IDLE_FLOOR_1;
String operationMode     = "AUTO"; // "AUTO" atau "MANUAL"
String positionStatus    = "FLOOR_1";
int    currentFloor      = 1;
String activeAlert       = "";
bool   motorEnabled      = false; // Status aktual pin ENABLE, untuk diagnostik

// Variabel Sensor (Hasil Filter Median)
float distU1 = 99.0;
float distU2 = 45.0;
float distU3 = 99.0;
float currentWeightGrams = 0.0;

// Buffer Filter Median (5 sampel per sensor)
float u1Samples[5] = {99, 99, 99, 99, 99};
float u2Samples[5] = {45, 45, 45, 45, 45};
float u3Samples[5] = {99, 99, 99, 99, 99};
int sampleIdxU1 = 0, sampleIdxU2 = 0, sampleIdxU3 = 0;

// Manajemen Stok (Disimpan di NVS Preferences)
int currentStockCount = 0;

// Status penghitung stok berbasis Ultrasonik 3
float         u3BaselineCm    = 80.0;  // Jarak acuan terakhir yang sudah dikonfirmasi
bool          u3BaselineReady = false;
float         u3CandidateCm   = 0.0;   // Jarak kandidat yang sedang diuji stabilitasnya
bool          u3Timing        = false;
unsigned long u3ChangeStartMs = 0;
bool          u3EmptyTiming   = false;
unsigned long u3EmptyStartMs  = 0;

// Timer Non-Blocking
unsigned long lastSensorPollMs    = 0;
int           sensorTurn          = 0; // Round-robin 0=U1, 1=U2, 2=U3
unsigned long itemDetectedStartMs = 0;
bool          isItemTiming        = false;
unsigned long unloadEmptyStartMs  = 0;
bool          isUnloadTiming      = false;
unsigned long lastTelemetryMs     = 0;
unsigned long lastMqttReconnectMs = 0;

// State LED
unsigned long lastLedBlinkMs = 0;
bool          redLedState    = false;

// ============================================================================
// 5. FUNGSI PEMBANTU: FILTER SENSOR & NVS STORAGE
// ============================================================================
float getMedian(float arr[], int size) {
  float temp[size];
  memcpy(temp, arr, size * sizeof(float));
  for (int i = 0; i < size - 1; i++) {
    for (int j = i + 1; j < size; j++) {
      if (temp[j] < temp[i]) {
        float t = temp[i];
        temp[i] = temp[j];
        temp[j] = t;
      }
    }
  }
  return temp[size / 2];
}

// Pembacaan ultrasonik dengan timeout ketat (15ms) agar tidak menghambat stepper
float readUltrasonicRaw(int trigPin, int echoPin) {
  digitalWrite(trigPin, LOW);
  delayMicroseconds(2);
  digitalWrite(trigPin, HIGH);
  delayMicroseconds(10);
  digitalWrite(trigPin, LOW);

  long duration = pulseIn(echoPin, HIGH, 15000); // Max ~250 cm, timeout 15ms
  if (duration == 0) return 99.9; // Tidak ada pantulan / di luar jangkauan
  return (duration * 0.0343) / 2.0;
}

void updateSensorsSequential() {
  // Baca satu sensor per siklus (setiap 35ms) agar stepper.run() tetap responsif
  if (millis() - lastSensorPollMs >= 35) {
    lastSensorPollMs = millis();

    if (sensorTurn == 0) {
      u1Samples[sampleIdxU1] = readUltrasonicRaw(TRIG_U1, ECHO_U1);
      sampleIdxU1 = (sampleIdxU1 + 1) % 5;
      distU1 = getMedian(u1Samples, 5);
    } else if (sensorTurn == 1) {
      u2Samples[sampleIdxU2] = readUltrasonicRaw(TRIG_U2, ECHO_U2);
      sampleIdxU2 = (sampleIdxU2 + 1) % 5;
      distU2 = getMedian(u2Samples, 5);
    } else if (sensorTurn == 2) {
      u3Samples[sampleIdxU3] = readUltrasonicRaw(TRIG_U3, ECHO_U3);
      sampleIdxU3 = (sampleIdxU3 + 1) % 5;
      distU3 = getMedian(u3Samples, 5);
    }
    sensorTurn = (sensorTurn + 1) % 3;
  }

  // Pembacaan Load Cell non-blocking
  if (scale.is_ready()) {
    float reading = scale.get_units(1);
    currentWeightGrams = (reading < 0) ? 0.0 : reading;
  }
}

void saveStockToNVS(int stock) {
  preferences.begin("warehouse", false);
  preferences.putInt("stock_count", stock);
  preferences.end();
}

int loadStockFromNVS() {
  preferences.begin("warehouse", true);
  int saved = preferences.getInt("stock_count", 0);
  preferences.end();
  return saved;
}

// Sisa slot dihitung dari sisa jarak: 80 cm = 4 slot kosong, 10 cm = 0 (penuh)
int calculateRemainingSlots() {
  if (distU3 <= U3_FULL_CM) return 0;
  if (distU3 >= U3_EMPTY_CM) return MAX_CAPACITY_SLOTS;
  int slots = floor((distU3 - U3_FULL_CM) / SLOT_WIDTH_CM);
  return constrain(slots, 0, MAX_CAPACITY_SLOTS);
}

// Hitung stok Lantai 2 dari perubahan jarak Ultrasonik 3
void updateStockFromU3() {
  unsigned long now = millis();
  if (now < 3000) return; // Tunggu buffer filter terisi setelah boot

  // Acuan awal: jarak saat boot (stok tersimpan di NVS tidak diubah)
  if (!u3BaselineReady) {
    u3BaselineCm = min(distU3, U3_EMPTY_CM);
    u3BaselineReady = true;
    return;
  }

  // Abaikan saat lift bergerak agar pembacaan tidak salah
  if (currentState == STATE_LIFT_ASCENDING || currentState == STATE_LIFT_DESCENDING ||
      currentState == STATE_MANUAL_JOG) {
    u3Timing = false;
    u3EmptyTiming = false;
    return;
  }

  // Jarak > 80 cm selama 5 detik = ruang kosong -> stok 0
  if (distU3 > U3_EMPTY_CM) {
    u3Timing = false;
    if (!u3EmptyTiming) {
      u3EmptyTiming = true;
      u3EmptyStartMs = now;
    } else if (now - u3EmptyStartMs >= U3_CONFIRM_MS) {
      u3EmptyStartMs = now;
      u3BaselineCm = U3_EMPTY_CM;
      if (currentStockCount != 0) {
        currentStockCount = 0;
        saveStockToNVS(currentStockCount);
        publishTelemetry();
      }
    }
    return;
  }
  u3EmptyTiming = false;

  // diff > 0: jarak berkurang (barang masuk), diff < 0: jarak bertambah (barang keluar)
  float diff = u3BaselineCm - distU3;
  if (fabs(diff) >= U3_CHANGE_MIN_CM) {
    if (!u3Timing || fabs(distU3 - u3CandidateCm) > U3_STABLE_BAND_CM) {
      u3Timing = true;               // Mulai / ulang hitung 5 detik
      u3CandidateCm = distU3;
      u3ChangeStartMs = now;
    } else if (now - u3ChangeStartMs >= U3_CONFIRM_MS) {
      u3Timing = false;
      if (diff > 0) {
        if (currentStockCount < MAX_CAPACITY_SLOTS) currentStockCount++;
      } else {
        if (currentStockCount > 0) currentStockCount--;
      }
      u3BaselineCm = distU3;         // Jadikan jarak baru sebagai acuan
      saveStockToNVS(currentStockCount);
      publishTelemetry();
    }
  } else {
    u3Timing = false;
  }
}

// ============================================================================
// 6. KONTROL MOTOR STEPPER, LED & TELEMETRI MQTT
// ============================================================================
void enableMotor(bool enable) {
  // Driver A4988/DRV8825/TMC2209 menggunakan logika Active LOW pada pin ENABLE
  digitalWrite(PIN_ENABLE, enable ? LOW : HIGH);
  motorEnabled = enable;
}

void updateLeds() {
  // Hijau: menyala selama lift bergerak (naik, turun, atau jog manual)
  bool moving = (currentState == STATE_LIFT_ASCENDING ||
                 currentState == STATE_LIFT_DESCENDING ||
                 currentState == STATE_MANUAL_JOG);
  digitalWrite(PIN_LED_GREEN, moving ? HIGH : LOW);

  // Merah: berkedip saat emergency (termasuk timeout alignment), selain itu mati
  if (currentState == STATE_EMERGENCY_HALT) {
    if (millis() - lastLedBlinkMs >= LED_BLINK_INTERVAL_MS) {
      lastLedBlinkMs = millis();
      redLedState = !redLedState;
      digitalWrite(PIN_LED_RED, redLedState ? HIGH : LOW);
    }
  } else {
    redLedState = false;
    digitalWrite(PIN_LED_RED, LOW);
  }
}

void publishTelemetry() {
  if (!mqttClient.connected()) return;

  JsonDocument doc;
  doc["device_id"]      = DEVICE_ID;
  doc["operation_mode"] = operationMode;

  // Lift State
  JsonObject lift = doc["lift_state"].to<JsonObject>();
  lift["current_floor"]           = currentFloor;
  lift["position_status"]         = positionStatus;
  lift["ultrasonic_2_distance_cm"] = round(distU2 * 10.0) / 10.0;
  lift["is_aligned"]              = (positionStatus == "FLOOR_1" || positionStatus == "FLOOR_2");
  lift["motor_enabled"]           = motorEnabled; // Diagnostik: status aktual pin ENABLE driver
  lift["stepper_steps_remaining"] = stepper.distanceToGo(); // Diagnostik: sisa langkah tujuan saat ini

  // Stock & Floor 2 Space
  int remainingSlots = calculateRemainingSlots();
  JsonObject stock = doc["stock_management"].to<JsonObject>();
  stock["current_stock_count"]      = currentStockCount;
  stock["max_capacity_slots"]       = MAX_CAPACITY_SLOTS;
  stock["remaining_empty_slots"]    = remainingSlots;
  stock["ultrasonic_3_distance_cm"] = round(distU3 * 10.0) / 10.0;
  stock["storage_status"]           = (remainingSlots > 0) ? "AVAILABLE" : "FULL";

  // Cargo Sensor
  JsonObject cargo = doc["cargo_sensor"].to<JsonObject>();
  cargo["item_on_lift_u1"]        = (distU1 < THRESHOLD_ITEM_CM);
  cargo["ultrasonic_1_distance_cm"] = round(distU1 * 10.0) / 10.0; // Diagnostik: nilai mentah sensor U1
  cargo["weight_grams"]           = round(currentWeightGrams * 10.0) / 10.0;
  cargo["max_weight_limit_grams"] = MAX_WEIGHT_GRAMS;
  cargo["is_overweight"]          = (currentWeightGrams > MAX_WEIGHT_GRAMS);

  if (activeAlert.length() > 0) {
    doc["active_alert"] = activeAlert;
  } else {
    doc["active_alert"] = nullptr;
  }

  char buffer[1024];
  serializeJson(doc, buffer);
  // Retained = true agar web dashboard langsung mendapat state terakhir saat dibuka
  mqttClient.publish(TOPIC_TELEMETRY, buffer, true);
}

void sendCommandAck(String cmdId, String status, String reasonCode, String message) {
  if (!mqttClient.connected()) return;

  JsonDocument doc;
  doc["command_id"]  = cmdId;
  doc["status"]      = status;
  doc["reason_code"] = reasonCode;
  doc["message"]     = message;

  char buffer[512];
  serializeJson(doc, buffer);
  mqttClient.publish(TOPIC_ACK, buffer, false);
}

void startMoveToFloor2() {
  enableMotor(true);
  stepper.setMaxSpeed(SPEED_NORMAL);
  stepper.setAcceleration(ACCEL_NORMAL);
  stepper.setCurrentPosition(0);
  stepper.moveTo(MAX_TRAVEL_STEPS); // Arah naik (sesuaikan tanda +/- jika arah terbalik)

  currentState   = STATE_LIFT_ASCENDING;
  positionStatus = "MOVING_TO_FLOOR_2";
  activeAlert    = "";
  publishTelemetry();
}

void startMoveToFloor1() {
  enableMotor(true);
  stepper.setMaxSpeed(SPEED_NORMAL);
  stepper.setAcceleration(ACCEL_NORMAL);
  stepper.setCurrentPosition(0);
  stepper.moveTo(-MAX_TRAVEL_STEPS); // Arah turun

  currentState   = STATE_LIFT_DESCENDING;
  positionStatus = "MOVING_TO_FLOOR_1";
  activeAlert    = "";
  publishTelemetry();
}

// ============================================================================
// 7. HANDLER PERINTAH MQTT DARI WEB DASHBOARD
// ============================================================================
void mqttCallback(char* topic, byte* payload, unsigned int length) {
  JsonDocument doc;
  DeserializationError error = deserializeJson(doc, payload, length);
  if (error) {
    Serial.print("Gagal parsing JSON command: ");
    Serial.println(error.c_str());
    return;
  }

  String cmdId  = doc["command_id"] | "CMD-UNKNOWN";
  String action = doc["action"] | "";

  // 1. EMERGENCY STOP (Prioritas Tertinggi)
  if (action == "CMD_EMERGENCY_STOP") {
    stepper.stop();
    stepper.setCurrentPosition(0);
    enableMotor(false);
    currentState   = STATE_EMERGENCY_HALT;
    positionStatus = "MID_TRANSIT_STOPPED";
    activeAlert    = "EMERGENCY_STOP_TRIGGERED";
    sendCommandAck(cmdId, "EXECUTED", "OK", "Emergency stop diaktifkan. Motor dihentikan.");
    publishTelemetry();
    return;
  }

  // 2. RESET ALARM / EMERGENCY
  if (action == "CMD_RESET_ALARM") {
    activeAlert = "";
    if (abs(distU2 - DIST_U2_FLOOR_1_CM) <= ALIGN_TOLERANCE_CM) {
      currentState = STATE_IDLE_FLOOR_1;
      positionStatus = "FLOOR_1";
      currentFloor = 1;
    } else if (abs(distU2 - DIST_U2_FLOOR_2_CM) <= ALIGN_TOLERANCE_CM) {
      currentState = STATE_UNLOADING_FLOOR_2;
      positionStatus = "FLOOR_2";
      currentFloor = 2;
    } else {
      currentState = STATE_IDLE_FLOOR_1;
    }
    sendCommandAck(cmdId, "EXECUTED", "OK", "Status alarm berhasil di-reset.");
    publishTelemetry();
    return;
  }

  // 3. UBAH MODE OPERASI (AUTO / MANUAL)
  if (action == "CMD_SET_MODE") {
    String newMode = doc["payload"]["mode"] | "AUTO";
    if (newMode == "AUTO" || newMode == "MANUAL") {
      operationMode = newMode;
      sendCommandAck(cmdId, "EXECUTED", "OK", "Mode diubah ke " + operationMode);
      publishTelemetry();
    } else {
      sendCommandAck(cmdId, "REJECTED", "INVALID_MODE", "Mode tidak dikenal.");
    }
    return;
  }

  // 4. PERINTAH NAIK KE LANTAI 2 VIA WEB
  if (action == "CMD_MOVE_FLOOR_2") {
    if (currentState == STATE_EMERGENCY_HALT) {
      sendCommandAck(cmdId, "REJECTED", "ERR_EMERGENCY_LOCKED", "Reset emergency stop terlebih dahulu.");
      return;
    }
    if (currentWeightGrams > MAX_WEIGHT_GRAMS) {
      sendCommandAck(cmdId, "REJECTED", "ERR_OVERWEIGHT", "Beban melebihi batas maksimal 3 kg.");
      return;
    }
    bool forceOverride = doc["payload"]["force_override"] | false;
    if (!forceOverride && calculateRemainingSlots() <= 0) {
      sendCommandAck(cmdId, "REJECTED", "ERR_FLOOR_2_FULL", "Ruang penyimpanan Lantai 2 penuh.");
      return;
    }
    startMoveToFloor2();
    sendCommandAck(cmdId, "EXECUTED", "OK", "Lift bergerak naik menuju Lantai 2.");
    return;
  }

  // 5. PERINTAH TURUN KE LANTAI 1 VIA WEB
  if (action == "CMD_MOVE_FLOOR_1") {
    if (currentState == STATE_EMERGENCY_HALT) {
      sendCommandAck(cmdId, "REJECTED", "ERR_EMERGENCY_LOCKED", "Reset emergency stop terlebih dahulu.");
      return;
    }
    startMoveToFloor1();
    sendCommandAck(cmdId, "EXECUTED", "OK", "Lift bergerak turun menuju Lantai 1.");
    return;
  }

  // 6. TARE / KALIBRASI NOL LOAD CELL
  if (action == "CMD_TARE_LOADCELL") {
    if (distU1 >= THRESHOLD_ITEM_CM) {
      scale.tare(10);
      currentWeightGrams = 0.0;
      sendCommandAck(cmdId, "EXECUTED", "OK", "Load cell berhasil di-tare (nol).");
      publishTelemetry();
    } else {
      sendCommandAck(cmdId, "REJECTED", "ERR_PLATFORM_NOT_EMPTY", "Kosongkan barang sebelum tare.");
    }
    return;
  }

  // 7. JOG MANUAL MOTOR (DIAGNOSTIK) - Menggerakkan stepper langkah kecil, abaikan sensor lantai
  //    payload: {"direction":"UP"|"DOWN", "steps": 200}
  if (action == "CMD_JOG") {
    String direction = doc["payload"]["direction"] | "UP";
    long steps = doc["payload"]["steps"] | 200;
    steps = constrain(steps, 1L, 3000L); // Batas aman per jog agar tidak kebablasan

    long target = (direction == "DOWN") ? -steps : steps;

    enableMotor(true);
    stepper.setMaxSpeed(SPEED_APPROACH); // Pakai kecepatan pelan untuk tes manual, lebih aman
    stepper.setAcceleration(ACCEL_NORMAL);
    stepper.setCurrentPosition(0);
    stepper.moveTo(target);

    currentState   = STATE_MANUAL_JOG;
    positionStatus = (direction == "DOWN") ? "JOG_DOWN" : "JOG_UP";
    sendCommandAck(cmdId, "EXECUTED", "OK", "Jog manual " + direction + " sejauh " + String(steps) + " langkah.");
    publishTelemetry();
    return;
  }

  // 8. BACA ULANG LOAD CELL SEKARANG (DIAGNOSTIK) - Paksa publish telemetri terbaru di luar siklus 2 detik
  if (action == "CMD_READ_SENSORS_NOW") {
    publishTelemetry();
    sendCommandAck(cmdId, "EXECUTED", "OK", "Telemetri terbaru dikirim.");
    return;
  }

  // 9. UPDATE / KOREKSI STOK BARANG DARI WEB (Misal: Barang diambil dari Lantai 2)
  if (action == "CMD_UPDATE_STOCK") {
    int delta = doc["payload"]["delta"] | 0;
    bool resetZero = doc["payload"]["reset"] | false;

    if (resetZero) {
      currentStockCount = 0;
    } else {
      currentStockCount = max(0, currentStockCount + delta);
    }
    saveStockToNVS(currentStockCount);
    sendCommandAck(cmdId, "EXECUTED", "OK", "Stok diperbarui menjadi " + String(currentStockCount));
    publishTelemetry();
    return;
  }
}

// ============================================================================
// 8. KONEKSI WIFI & MQTT NON-BLOCKING
// ============================================================================
void handleConnections() {
  if (WiFi.status() != WL_CONNECTED) return;

  if (!mqttClient.connected()) {
    if (millis() - lastMqttReconnectMs > 5000) {
      lastMqttReconnectMs = millis();
      Serial.print("Menghubungkan ke MQTT Broker...");

      // Konfigurasi Last Will and Testament (LWT) jika ESP32 putus koneksi mendadak
      if (mqttClient.connect(DEVICE_ID, TOPIC_STATUS, 1, true, "{\"status\":\"OFFLINE\"}")) {
        Serial.println("Terhubung!");
        mqttClient.publish(TOPIC_STATUS, "{\"status\":\"ONLINE\"}", true);
        mqttClient.subscribe(TOPIC_COMMAND, 1);
        publishTelemetry();
      } else {
        Serial.print("Gagal, rc=");
        Serial.println(mqttClient.state());
      }
    }
  } else {
    mqttClient.loop();
  }
}

// ============================================================================
// 9. LOGIKA FINITE STATE MACHINE (FSM) UTAMA
// ============================================================================
void runStateMachine() {
  switch (currentState) {

    // ------------------------------------------------------------------------
    case STATE_IDLE_FLOOR_1:
      // Hanya berjalan otomatis jika mode == AUTO
      if (operationMode == "AUTO") {
        if (distU1 < THRESHOLD_ITEM_CM) {
          if (!isItemTiming) {
            isItemTiming = true;
            itemDetectedStartMs = millis();
          } else if (millis() - itemDetectedStartMs >= STABLE_ITEM_MS) {
            isItemTiming = false;
            currentState = STATE_WEIGHING_AND_CHECK;
          }
        } else {
          isItemTiming = false;
        }
      }
      break;

    // ------------------------------------------------------------------------
    case STATE_WEIGHING_AND_CHECK:
      // Validasi 1: Apakah barang masih ada?
      if (distU1 >= THRESHOLD_ITEM_CM) {
        currentState = STATE_IDLE_FLOOR_1;
        break;
      }

      // Validasi 2: Cek batas berat Load Cell
      if (currentWeightGrams > MAX_WEIGHT_GRAMS) {
        currentState = STATE_ERROR_OVERWEIGHT;
        activeAlert  = "ERR_OVERWEIGHT";
        publishTelemetry();
        break;
      }

      // Validasi 3: Cek ketersediaan ruang Lantai 2 via Ultrasonik 3
      if (calculateRemainingSlots() <= 0) {
        currentState = STATE_WAIT_FLOOR_2_SPACE;
        activeAlert  = "ERR_FLOOR_2_FULL";
        publishTelemetry();
        break;
      }

      // Semua validasi lolos -> Naikkan lift ke Lantai 2
      startMoveToFloor2();
      break;

    // ------------------------------------------------------------------------
    case STATE_ERROR_OVERWEIGHT:
      // Pulih otomatis jika beban berlebih diangkat dari platform
      if (currentWeightGrams <= MAX_WEIGHT_GRAMS) {
        activeAlert  = "";
        currentState = STATE_IDLE_FLOOR_1;
        publishTelemetry();
      }
      break;

    // ------------------------------------------------------------------------
    case STATE_WAIT_FLOOR_2_SPACE:
      // Pulih otomatis jika barang dibatalkan dari Lantai 1 atau ruang Lantai 2 dikosongkan
      if (distU1 >= THRESHOLD_ITEM_CM) {
        activeAlert  = "";
        currentState = STATE_IDLE_FLOOR_1;
        publishTelemetry();
      } else if (calculateRemainingSlots() > 0 && operationMode == "AUTO") {
        activeAlert  = "";
        startMoveToFloor2();
      }
      break;

    // ------------------------------------------------------------------------
    case STATE_LIFT_ASCENDING: {
      float distanceToTarget = distU2 - DIST_U2_FLOOR_2_CM;

      // Perlambat kecepatan saat memasuki zona pendekatan (5 cm sebelum Lantai 2)
      if (distanceToTarget <= SLOWDOWN_ZONE_CM && distanceToTarget > ALIGN_TOLERANCE_CM) {
        stepper.setMaxSpeed(SPEED_APPROACH);
      }

      // Cek ketepatan posisi berhenti via Ultrasonik 2
      if (distU2 <= (DIST_U2_FLOOR_2_CM + ALIGN_TOLERANCE_CM)) {
        stepper.setCurrentPosition(0); // Hentikan langkah segera dengan presisi
        stepper.moveTo(0);
        // Biarkan motor tetap enabled (holding torque) agar platform tidak melorot saat bongkar muat
        currentFloor   = 2;
        positionStatus = "FLOOR_2";
        currentState   = STATE_UNLOADING_FLOOR_2;
        isUnloadTiming = false;
        publishTelemetry();
      }
      // Failsafe: Jika batas langkah maksimal tercapai tapi sensor belum mendeteksi lantai
      else if (stepper.distanceToGo() == 0) {
        enableMotor(false);
        currentState   = STATE_EMERGENCY_HALT;
        positionStatus = "MID_TRANSIT_STOPPED";
        activeAlert    = "ERR_ALIGNMENT_TIMEOUT";
        publishTelemetry();
      }
      break;
    }

    // ------------------------------------------------------------------------
    case STATE_UNLOADING_FLOOR_2:
      if (operationMode == "AUTO") {
        // Tunggu sampai barang dipindahkan dari platform (Ultrasonik 1 kosong & berat ~ 0g)
        bool platformEmpty = (distU1 >= THRESHOLD_ITEM_CM) && (currentWeightGrams <= EMPTY_WEIGHT_GRAMS);

        if (platformEmpty) {
          if (!isUnloadTiming) {
            isUnloadTiming = true;
            unloadEmptyStartMs = millis();
          } else if (millis() - unloadEmptyStartMs >= 3000) { // Jeda aman 3 detik
            isUnloadTiming = false;

            // Stok Lantai 2 dihitung otomatis dari Ultrasonik 3 (updateStockFromU3)

            // Turunkan lift kembali ke Lantai 1
            startMoveToFloor1();
          }
        } else {
          isUnloadTiming = false;
        }
      }
      break;

    // ------------------------------------------------------------------------
    case STATE_LIFT_DESCENDING: {
      float distanceToHome = DIST_U2_FLOOR_1_CM - distU2;

      // Perlambat kecepatan saat mendekati Lantai 1
      if (distanceToHome <= SLOWDOWN_ZONE_CM && distanceToHome > ALIGN_TOLERANCE_CM) {
        stepper.setMaxSpeed(SPEED_APPROACH);
      }

      // Cek ketepatan posisi berhenti di Lantai 1 via Ultrasonik 2
      if (distU2 >= (DIST_U2_FLOOR_1_CM - ALIGN_TOLERANCE_CM)) {
        stepper.setCurrentPosition(0);
        stepper.moveTo(0);
        enableMotor(false); // Matikan arus coil motor saat standby di dasar

        currentFloor   = 1;
        positionStatus = "FLOOR_1";
        currentState   = STATE_IDLE_FLOOR_1;

        // Auto-tare ringan untuk menjaga akurasi timbangan nol
        if (currentWeightGrams < 15.0) {
          scale.tare(5);
        }
        publishTelemetry();
      }
      else if (stepper.distanceToGo() == 0) {
        enableMotor(false);
        currentState   = STATE_EMERGENCY_HALT;
        positionStatus = "MID_TRANSIT_STOPPED";
        activeAlert    = "ERR_HOME_ALIGNMENT_TIMEOUT";
        publishTelemetry();
      }
      break;
    }

    // ------------------------------------------------------------------------
    case STATE_EMERGENCY_HALT:
      // Menunggu perintah CMD_RESET_ALARM dari Web Dashboard
      break;

    // ------------------------------------------------------------------------
    case STATE_MANUAL_JOG:
      // Tunggu jog manual selesai, lalu kembalikan ke status idle berdasarkan posisi sensor saat ini
      if (stepper.distanceToGo() == 0) {
        if (abs(distU2 - DIST_U2_FLOOR_1_CM) <= ALIGN_TOLERANCE_CM) {
          positionStatus = "FLOOR_1";
          currentFloor   = 1;
        } else if (abs(distU2 - DIST_U2_FLOOR_2_CM) <= ALIGN_TOLERANCE_CM) {
          positionStatus = "FLOOR_2";
          currentFloor   = 2;
        } else {
          positionStatus = "MID_TRANSIT_STOPPED";
        }
        currentState = STATE_IDLE_FLOOR_1;
        publishTelemetry();
      }
      break;
  }
}

// ============================================================================
// 10. SETUP & MAIN LOOP
// ============================================================================
void setup() {
  Serial.begin(115200);

  // Setup Pin Motor
  pinMode(PIN_ENABLE, OUTPUT);
  enableMotor(false);
  stepper.setMaxSpeed(SPEED_NORMAL);
  stepper.setAcceleration(ACCEL_NORMAL);

  // Setup LED Indikator
  pinMode(PIN_LED_RED, OUTPUT);
  pinMode(PIN_LED_GREEN, OUTPUT);
  digitalWrite(PIN_LED_RED, LOW);
  digitalWrite(PIN_LED_GREEN, LOW);

  // Setup Pin Ultrasonik
  pinMode(TRIG_U1, OUTPUT); pinMode(ECHO_U1, INPUT);
  pinMode(TRIG_U2, OUTPUT); pinMode(ECHO_U2, INPUT);
  pinMode(TRIG_U3, OUTPUT); pinMode(ECHO_U3, INPUT);

  // Setup Load Cell HX711
  // CATATAN: tare(10) bawaan library bersifat blocking - kalau HX711 belum
  // terdeteksi (belum tersambung/belum dapat power), ia bisa hang selamanya
  // dan membuat WiFi/MQTT/motor tidak pernah jalan. Pakai wait_ready_timeout
  // agar setup() tetap lanjut meski HX711 bermasalah.
  scale.begin(PIN_HX_DOUT, PIN_HX_SCK);
  scale.set_scale(LOADCELL_CALIBRATION);
  if (scale.wait_ready_timeout(2000)) {
    scale.tare(10);
    Serial.println("HX711 terdeteksi, tare berhasil.");
  } else {
    Serial.println("PERINGATAN: HX711 tidak terdeteksi dalam 2 detik - cek wiring DOUT/SCK/VCC/GND. Melanjutkan setup tanpa tare.");
  }

  // Muat sisa stok terakhir dari NVS Flash
  currentStockCount = loadStockFromNVS();
  Serial.printf("Stok awal dimuat dari NVS: %d unit\n", currentStockCount);

  // Setup WiFi
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  Serial.print("Menghubungkan WiFi");
  int retry = 0;
  while (WiFi.status() != WL_CONNECTED && retry < 20) {
    delay(500);
    Serial.print(".");
    retry++;
  }
  Serial.println();

  // Setup MQTT (Wajib perbesar buffer karena payload JSON > 256 byte)
  mqttClient.setServer(MQTT_BROKER, MQTT_PORT);
  mqttClient.setBufferSize(1024);
  mqttClient.setCallback(mqttCallback);
}

void loop() {
  // 1. PRIORITAS UTAMA: Jalankan pulsa stepper motor sesering mungkin
  stepper.run();

  // 2. Baca sensor ultrasonik & load cell secara non-blocking
  updateSensorsSequential();

  // 3. Jalankan logika Finite State Machine
  runStateMachine();

  // 3a. Hitung stok Lantai 2 dari sisa jarak Ultrasonik 3
  updateStockFromU3();

  // 3b. Update LED indikator (merah kedip = emergency, hijau = lift bergerak)
  updateLeds();

  // 4. Kelola koneksi WiFi & MQTT
  handleConnections();

  // 5. Kirim telemetri berkala setiap 2 detik untuk update dashboard web
  if (millis() - lastTelemetryMs >= 2000) {
    lastTelemetryMs = millis();
    publishTelemetry();
  }
}
