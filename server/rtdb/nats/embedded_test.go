package nats

import (
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type testEmbeddedServer struct {
	server *server.Server
	client *nats.Conn
	js     nats.JetStreamContext
}

func (es *testEmbeddedServer) Shutdown() {
	if es.client != nil {
		es.client.Close()
	}
	if es.server != nil {
		es.server.Shutdown()
	}
}

func (es *testEmbeddedServer) ClientURL() string {
	return es.server.ClientURL()
}

func (es *testEmbeddedServer) Conn() *nats.Conn {
	return es.client
}

func (es *testEmbeddedServer) JetStream() nats.JetStreamContext {
	return es.js
}

func (es *testEmbeddedServer) Publish(subject string, data []byte) error {
	return es.client.Publish(subject, data)
}

func (es *testEmbeddedServer) Subscribe(subject string, cb nats.MsgHandler) (*nats.Subscription, error) {
	return es.client.Subscribe(subject, cb)
}

type testConfig struct {
	Port     int
	StoreDir string
}

func testDefaultConfig() testConfig {
	return testConfig{
		Port:     -1,
		StoreDir: "./nats-store",
	}
}

func newTestEmbeddedServer(cfg testConfig) (*testEmbeddedServer, error) {
	opts := &server.Options{
		Port:       cfg.Port,
		StoreDir:   cfg.StoreDir,
		JetStream:  true,
		NoLog:      true,
		MaxPayload: 8 * 1024 * 1024,
	}

	s, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS server: %w", err)
	}

	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		s.Shutdown()
		return nil, fmt.Errorf("NATS server failed to start")
	}

	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		s.Shutdown()
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		s.Shutdown()
		return nil, fmt.Errorf("failed to initialize JetStream: %w", err)
	}

	return &testEmbeddedServer{
		server: s,
		client: nc,
		js:     js,
	}, nil
}