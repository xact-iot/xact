package mqtt

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/rtdb/ingest"
	"github.com/xact-iot/xact/rtdb/tree"
)

const (
	DefaultWorkerCount = 4
	DefaultQueueSize   = 1000
	DefaultBrokerURL   = "tcp://127.0.0.1:1883"
	DefaultEnqueueWait = 30 * time.Second
	MetricsInterval    = 500 * time.Millisecond
)

// Client is an MQTT client that ingests device data into the RTDB.
type Client struct {
	brokerURL  string
	clientID   string
	username   string
	password   string
	client     mqtt.Client
	treeOps    *tree.TreeWithOperations
	nc         *natsgo.Conn
	workerPool *WorkerPool
	metrics    *Metrics
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// ClientConfig holds configuration for the MQTT client.
type ClientConfig struct {
	BrokerURL   string
	ClientID    string
	Username    string
	Password    string
	WorkerCount int
	QueueSize   int
	EnqueueWait time.Duration
}

// NewClient creates a new MQTT client.
func NewClient(config ClientConfig, treeOps *tree.TreeWithOperations, nc *natsgo.Conn) *Client {
	if config.BrokerURL == "" {
		config.BrokerURL = DefaultBrokerURL
	}
	if config.ClientID == "" {
		config.ClientID = "rtdb-mqtt-client"
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = DefaultWorkerCount
	}
	if config.QueueSize <= 0 {
		config.QueueSize = DefaultQueueSize
	}
	if config.EnqueueWait <= 0 {
		config.EnqueueWait = DefaultEnqueueWait
	}

	metrics := &Metrics{}
	workerPool := NewWorkerPool(config.WorkerCount, config.QueueSize, config.EnqueueWait, nc, metrics)

	return &Client{
		brokerURL:  config.BrokerURL,
		clientID:   config.ClientID,
		username:   config.Username,
		password:   config.Password,
		treeOps:    treeOps,
		nc:         nc,
		workerPool: workerPool,
		metrics:    metrics,
		stopCh:     make(chan struct{}),
	}
}

// Start connects to the broker, subscribes, and starts processing workers.
func (c *Client) Start() error {
	c.workerPool.Start()

	if err := c.connect(); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}
	if err := c.subscribe(); err != nil {
		return fmt.Errorf("failed to subscribe to topics: %w", err)
	}

	log.Printf("MQTT client started: connected to %s, %d workers", c.brokerURL, c.workerPool.workers)
	return nil
}

// Stop disconnects and shuts down all goroutines.
func (c *Client) Stop() {
	close(c.stopCh)
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
	}
	c.workerPool.Stop()
	c.wg.Wait()
	log.Println("MQTT client stopped")
}

func (c *Client) connect() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(c.brokerURL)
	opts.SetClientID(c.clientID)
	if c.username != "" {
		opts.SetUsername(c.username)
	}
	if c.password != "" {
		opts.SetPassword(c.password)
	}
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetAutoAckDisabled(true)
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		log.Println("MQTT client: connected to broker")
		c.subscribe() //nolint:errcheck
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Printf("MQTT client: connection lost: %v", err)
	})

	c.client = mqtt.NewClient(opts)
	token := c.client.Connect()
	token.Wait()
	return token.Error()
}

func (c *Client) subscribe() error {
	token := c.client.Subscribe(TopicPattern, 1, c.messageHandler)
	token.Wait()
	if token.Error() != nil {
		return token.Error()
	}
	log.Printf("MQTT client: subscribed to %s (zoneless)", TopicPattern)

	tokenZoned := c.client.Subscribe(TopicPatternZoned, 1, c.messageHandler)
	tokenZoned.Wait()
	if tokenZoned.Error() != nil {
		return tokenZoned.Error()
	}
	log.Printf("MQTT client: subscribed to %s (zoned)", TopicPatternZoned)

	return nil
}

func (c *Client) messageHandler(_ mqtt.Client, msg mqtt.Message) {
	tenant, zone, msgType, deviceType, deviceName, err := ParseTopic(msg.Topic())
	if err != nil {
		log.Printf("MQTT client: bad topic %s: %v", msg.Topic(), err)
		msg.Ack()
		return
	}
	if msgType != "data" {
		msg.Ack()
		return
	}

	tagData, err := ParsePayload(msg.Payload())
	if err != nil {
		log.Printf("MQTT client: bad payload from %s: %v", msg.Topic(), err)
		msg.Ack()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.workerPool.enqueueWait)
	defer cancel()
	if !c.workerPool.SubmitContext(ctx, Message{
		Topic:      msg.Topic(),
		Tenant:     tenant,
		Zone:       zone,
		MsgType:    msgType,
		DeviceType: deviceType,
		DeviceName: deviceName,
		TagData:    tagData,
		Ack:        msg.Ack,
	}) {
		log.Printf("MQTT client: ingest queue busy, leaving message unacked from topic %s", msg.Topic())
	}
}

// SnapshotIngest satisfies ingest.IngestSampler, delegating to the worker pool metrics.
func (c *Client) SnapshotIngest() ingest.IngestSnapshot {
	return c.metrics.SnapshotIngest()
}

// NewClientFromEnv creates an MQTT client from environment variables.
func NewClientFromEnv(treeOps *tree.TreeWithOperations, nc *natsgo.Conn) *Client {
	config := ClientConfig{
		BrokerURL: os.Getenv("MQTT_CLIENT_URL"),
		ClientID:  os.Getenv("MQTT_CLIENT_ID"),
		Username:  os.Getenv("MQTT_CLIENT_USERNAME"),
		Password:  os.Getenv("MQTT_BROKER_PASSWORD"),
	}
	if workers := os.Getenv("MQTT_CLIENT_WORKERS"); workers != "" {
		fmt.Sscanf(workers, "%d", &config.WorkerCount) //nolint:errcheck
	}
	if qs := os.Getenv("MQTT_CLIENT_QUEUE_SIZE"); qs != "" {
		fmt.Sscanf(qs, "%d", &config.QueueSize) //nolint:errcheck
	}
	if waitMs := os.Getenv("MQTT_CLIENT_ENQUEUE_TIMEOUT_MS"); waitMs != "" {
		var ms int
		if _, err := fmt.Sscanf(waitMs, "%d", &ms); err == nil && ms > 0 {
			config.EnqueueWait = time.Duration(ms) * time.Millisecond
		}
	}
	return NewClient(config, treeOps, nc)
}
