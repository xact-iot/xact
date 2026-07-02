package traffic_images_driver

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	DefaultPollInterval = 3 * time.Minute // Must be less than 5 minutes to avoid timeout
)

func Start() {
	log.Println("[TrafficImages] Starting LTA Traffic Images driver")

	apiKey := os.Getenv("LTA_API_KEY")
	mqttBroker := mqttBrokerFromEnv()
	mqttUsername := os.Getenv("MQTT_BROKER_USERNAME")

	mqttPassword := os.Getenv("MQTT_BROKER_PASSWORD")
	if mqttPassword == "" {
		mqttPassword = "xact"
	}

	pollInterval := DefaultPollInterval
	if interval := os.Getenv("TRAFFIC_IMAGES_POLL_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			pollInterval = d
		}
	}

	client := NewTrafficImagesClient(apiKey)
	fmt.Printf("[TrafficImages] Using configured LTA API key and MQTT password\n")

	publisher := NewMQTTPublisher(mqttBroker, mqttUsername, mqttPassword)
	if err := publisher.Connect(); err != nil {
		log.Fatalf("[TrafficImages] Failed to connect to MQTT broker %s: %v", mqttBroker, err)
	}
	defer publisher.Disconnect()
	fmt.Printf("[TrafficImages] Connected to MQTT broker at %s\n", mqttBroker)

	log.Printf("[TrafficImages] Poll interval %v\n", pollInterval)
	ticker := time.NewTicker(pollInterval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	pollAndPublish(client, publisher)

	for {
		select {
		case <-ticker.C:
			pollAndPublish(client, publisher)
		case <-sigCh:
			ticker.Stop()
			return
		}
	}
}

func mqttBrokerFromEnv() string {
	if broker := os.Getenv("MQTT_BROKER_URL"); broker != "" {
		return broker
	}
	if broker := os.Getenv("MQTT_BROKER"); broker != "" {
		return broker
	}
	return "tcp://127.0.0.1:1883"
}

func pollAndPublish(client *TrafficImagesClient, publisher *MQTTPublisher) {
	fmt.Printf("Polling traffic images at %s\n", time.Now().Format(time.RFC3339))
	data, err := client.FetchTrafficImages()
	if err != nil {
		log.Printf("[TrafficImages] Failed to fetch traffic images: %v", err)
		return
	}
	log.Printf("[TrafficImages] Fetched %d Camera images\n", len(data))
	if err := publisher.PublishTrafficImages(data); err != nil {
		log.Printf("[TrafficImages] Failed to publish traffic images: %v", err)
	}
}
