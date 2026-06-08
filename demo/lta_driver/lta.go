package lta_driver

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	// DefaultPollInterval is the time between API polls
	DefaultPollInterval = 5 * time.Minute
)

func Start() {
	log.Println("Starting LTA driver")
	// Get configuration from environment (or use default)
	apiKey := os.Getenv("LTA_API_KEY")

	mqttBroker := os.Getenv("MQTT_BROKER")
	if mqttBroker == "" {
		mqttBroker = "tcp://127.0.0.1:1883"
	}

	mqttPassword := os.Getenv("MQTT_PASSWORD")
	if mqttPassword == "" {
		mqttPassword = "xact"
	}

	pollInterval := DefaultPollInterval
	if interval := os.Getenv("POLL_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			pollInterval = d
		}
	}

	// Create LTA API client
	vmsClient := NewVMSClient(apiKey)

	// Create MQTT publisher
	publisher := NewMQTTPublisher(mqttBroker, mqttPassword)
	if err := publisher.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	defer publisher.Disconnect()

	// Create ticker for polling
	ticker := time.NewTicker(pollInterval)

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Poll immediately on startup
	pollAndPublish(vmsClient, publisher)

	// Main loop
	for {
		select {
		case <-ticker.C:
			pollAndPublish(vmsClient, publisher)
		case <-sigCh:
			ticker.Stop()
			return
		}
	}
}

// pollAndPublish fetches VMS data and publishes it via MQTT
func pollAndPublish(vmsClient *VMSClient, publisher *MQTTPublisher) {
	data, err := vmsClient.FetchVMSData()
	if err != nil {
		log.Printf("Failed to fetch VMS data: %v", err)
		return
	}
	log.Printf("[VMS] Fetched %d VMS records\n", len(data))
	if err := publisher.PublishVMSData(data); err != nil {
		log.Printf("Failed to publish VMS data: %v", err)
	}
}
