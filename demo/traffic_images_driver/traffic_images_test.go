package traffic_images_driver

import "testing"

func TestMQTTBrokerFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		brokerURL    string
		legacyBroker string
		want         string
	}{
		{
			name:      "uses server broker url env",
			brokerURL: "mqtt://demo.example:1883",
			want:      "mqtt://demo.example:1883",
		},
		{
			name:         "falls back to legacy demo broker env",
			legacyBroker: "tcp://legacy.example:1883",
			want:         "tcp://legacy.example:1883",
		},
		{
			name:         "server broker url wins over legacy broker env",
			brokerURL:    "mqtt://server.example:1883",
			legacyBroker: "tcp://legacy.example:1883",
			want:         "mqtt://server.example:1883",
		},
		{
			name: "uses local default",
			want: "tcp://127.0.0.1:1883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MQTT_BROKER_URL", tt.brokerURL)
			t.Setenv("MQTT_BROKER", tt.legacyBroker)

			if got := mqttBrokerFromEnv(); got != tt.want {
				t.Fatalf("mqttBrokerFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}
