package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port            string
	APIToken        string
	DBUser          string
	DBPassword      string
	DBHost          string
	DBPort          string
	DBName          string
	MQTTBroker      string
	MQTTClientID    string
	MQTTUsername    string
	MQTTPassword    string
	EnableSimulator bool
}

func LoadConfig() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		APIToken:        getEnv("API_TOKEN", "smart-warehouse-secret-key-2026"),
		DBUser:          getEnv("DB_USER", "root"),
		DBPassword:      getEnv("DB_PASSWORD", ""),
		DBHost:          getEnv("DB_HOST", "127.0.0.1"),
		DBPort:          getEnv("DB_PORT", "3306"),
		DBName:          getEnv("DB_NAME", "smart_warehouse"),
		MQTTBroker:      getEnv("MQTT_BROKER", ""), // e.g. "ssl://xxxx.hivemq.cloud:8883" or "tcp://localhost:1883"
		MQTTClientID:    getEnv("MQTT_CLIENT_ID", "warehouse-backend"),
		MQTTUsername:    getEnv("MQTT_USERNAME", ""),
		MQTTPassword:    getEnv("MQTT_PASSWORD", ""),
		EnableSimulator: getEnvAsBool("ENABLE_SIMULATOR", true),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return defaultVal
}
