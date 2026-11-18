package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DeviceIP          string
	DevicePort        int
	BackendURL        string
	EnableLiveCapture bool
	ServerPort        string
	Debug             bool
	SecretKey         string
	DevicePass        int
}

func LoadConfig() *Config {
	// Load .env file if it exists, otherwise fall back to system environment variables
	if err := godotenv.Load(); err != nil {
		log.Println(".env file not found, reading from system environment")
	}

	devicePort, err := strconv.Atoi(getEnv("DEVICE_PORT", "4370"))
	if err != nil {
		devicePort = 4370
	}

	devicePass, err := strconv.Atoi(getEnv("DEVICE_PASSWORD", "0"))
	if err != nil {
		devicePass = 0
	}

	debug, _ := strconv.ParseBool(getEnv("FLASK_DEBUG", getEnv("DEBUG", "false")))

	serverPort := getEnv("PORT", "4000")
	backendURL := getEnv("BACKEND_URL", "")

	enableLiveCapture := false
	if backendURL != "" {
		enableLiveCapture, _ = strconv.ParseBool(getEnv("ENABLE_LIVE_CAPTURE", "true"))
	}

	return &Config{
		DeviceIP:          getEnv("DEVICE_IP", "192.168.3.18"),
		DevicePort:        devicePort,
		BackendURL:        backendURL,
		EnableLiveCapture: enableLiveCapture,
		ServerPort:        serverPort,
		Debug:             debug,
		SecretKey:         getEnv("SECRET_KEY", ""),
		DevicePass:        devicePass,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
