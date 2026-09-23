package lta_driver

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/xact-iot/xact/demo/internal/mqttclient"
)

// MQTTPublisher handles publishing VMS data to XACT via MQTT
type MQTTPublisher struct {
	client   mqtt.Client
	broker   string
	password string
	username string
}

// NewMQTTPublisher creates a new MQTT publisher
func NewMQTTPublisher(broker, password string) *MQTTPublisher {
	username := os.Getenv("MQTT_BROKER_USERNAME")
	if username == "" {
		username = "default"
	}
	return &MQTTPublisher{
		username: username,
		broker:   broker,
		password: password,
	}
}

// Connect establishes connection to the MQTT broker
func (p *MQTTPublisher) Connect() error {
	broker := mqttclient.NormalizeBrokerURL(p.broker)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	if tlsConfig := mqttclient.TLSConfigFromEnv(broker); tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}
	opts.SetClientID(p.username + ":lta-vms-driver")
	opts.SetUsername(p.username)
	opts.SetPassword(p.password)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(30 * time.Second)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
	})

	p.client = mqtt.NewClient(opts)

	token := p.client.Connect()
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	return nil
}

// Disconnect closes the MQTT connection
func (p *MQTTPublisher) Disconnect() {
	if p.client != nil && p.client.IsConnected() {
		p.client.Disconnect(250)
	}
}

// PublishVMS publishes a single VMS data point to XACT
func (p *MQTTPublisher) PublishVMS(data VMSData) error {
	// Build topic: xact/{tenant}/{msgtype}/{devicetype}/{devicename}
	// tenant: default
	// msgtype: data/cmd
	// devicetype: VMS
	// devicename: EquipmentID
	topic := fmt.Sprintf("xact/data/%s/zone/Singapore/VMS/%s", p.username, data.EquipmentID)

	// Build payload with nested taggroups
	// meta: lat, lon, ts
	// sign: message
	payload := map[string]interface{}{
		"meta": map[string]interface{}{
			"lat":    data.Latitude,
			"lon":    data.Longitude,
			"ts":     fmt.Sprintf("%d", time.Now().UnixMilli()),
			"online": true,
		},
		"sign": map[string]interface{}{
			"message": strings.ReplaceAll(data.Message, ",", "<br/>"),
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	token := p.client.Publish(topic, 1, false, jsonPayload)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish: %w", token.Error())
	}

	return nil
}

// PublishVMSData publishes multiple VMS data points
func (p *MQTTPublisher) PublishVMSData(data []VMSData) error {
	successCount := 0
	failCount := 0

	for _, vms := range data {
		if err := p.PublishVMS(vms); err != nil {
			log.Printf("Failed to publish VMS %s: %v", vms.EquipmentID, err)
			failCount++
			// Continue with next VMS even if one fails
		} else {
			successCount++
		}
	}
	// log.Printf("[VMS] RTDB update complete: %d successful, %d failed, %d total", successCount, failCount, len(data))
	return nil
}
