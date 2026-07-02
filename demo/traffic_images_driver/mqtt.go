package traffic_images_driver

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/xact-iot/xact/demo/internal/mqttclient"
)

const (
	mqttConnectTimeout = 10 * time.Second
	mqttPublishTimeout = 10 * time.Second
)

// MQTTPublisher handles publishing traffic image data to XACT via MQTT
type MQTTPublisher struct {
	client   mqtt.Client
	broker   string
	username string
	password string
}

// NewMQTTPublisher creates a new MQTT publisher
func NewMQTTPublisher(broker, username, password string) *MQTTPublisher {
	return &MQTTPublisher{
		broker:   broker,
		username: username,
		password: password,
	}
}

// Connect establishes connection to the MQTT broker
func (p *MQTTPublisher) Connect() error {
	broker := normalizeBrokerURL(p.broker)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	if tlsConfig := mqttclient.TLSConfigFromEnv(broker); tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}
	opts.SetClientID("lta-traffic-images-driver")
	if p.username != "" {
		opts.SetUsername(p.username)
	}
	if p.password != "" {
		opts.SetPassword(p.password)
	}
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(false)
	opts.SetConnectTimeout(mqttConnectTimeout)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(30 * time.Second)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("[TrafficImages] MQTT connection lost: %v", err)
	})

	p.client = mqtt.NewClient(opts)
	fmt.Printf("[TrafficImages] Connecting to MQTT broker at %s\n", broker)
	token := p.client.Connect()
	if !token.WaitTimeout(mqttConnectTimeout + time.Second) {
		p.client.Disconnect(250)
		return fmt.Errorf("timed out connecting to MQTT broker %s after %s", broker, mqttConnectTimeout)
	}
	if token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	return nil
}

func normalizeBrokerURL(broker string) string {
	return mqttclient.NormalizeBrokerURL(broker)
}

// Disconnect closes the MQTT connection
func (p *MQTTPublisher) Disconnect() {
	if p.client != nil && p.client.IsConnected() {
		p.client.Disconnect(250)
	}
}

// PublishTrafficImage publishes a single traffic camera image record
func (p *MQTTPublisher) PublishTrafficImage(data TrafficImageData) error {
	topic := fmt.Sprintf("xact/data/default/zone/Singapore/TrafficCamera/%s", data.CameraID)

	payload := map[string]any{
		"meta": map[string]any{
			"lat":    data.Latitude,
			"lon":    data.Longitude,
			"ts":     fmt.Sprintf("%d", time.Now().UnixMilli()),
			"online": true,
		},
		"image": map[string]any{
			"url": map[string]any{
				"value":      data.ImageLink,
				"stalecheck": 290,
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	token := p.client.Publish(topic, 1, false, jsonPayload)
	if !token.WaitTimeout(mqttPublishTimeout) {
		return fmt.Errorf("timed out publishing to MQTT topic %s after %s", topic, mqttPublishTimeout)
	}
	if token.Error() != nil {
		return fmt.Errorf("failed to publish: %w", token.Error())
	}

	return nil
}

// PublishTrafficImages publishes multiple traffic camera image records
func (p *MQTTPublisher) PublishTrafficImages(data []TrafficImageData) error {
	successCount := 0
	failCount := 0

	for _, img := range data {
		if err := p.PublishTrafficImage(img); err != nil {
			log.Printf("[TrafficImages] Failed to publish camera %s: %v", img.CameraID, err)
			failCount++
		} else {
			successCount++
		}
	}
	log.Printf("[TrafficImages] RTDB update complete: %d successful, %d failed, %d total",
		successCount, failCount, len(data))
	return nil
}
