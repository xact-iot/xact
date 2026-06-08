package traffic_images_driver

import "testing"

func TestNormalizeBrokerURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "server mqtt scheme", in: "mqtt://127.0.0.1:1883", want: "tcp://127.0.0.1:1883"},
		{name: "server secure mqtt scheme", in: "mqtts://broker.example:8883", want: "ssl://broker.example:8883"},
		{name: "paho tcp scheme", in: "tcp://127.0.0.1:1883", want: "tcp://127.0.0.1:1883"},
		{name: "bare host port", in: "127.0.0.1:1883", want: "tcp://127.0.0.1:1883"},
		{name: "trim whitespace", in: " mqtt://localhost:1883 ", want: "tcp://localhost:1883"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBrokerURL(tt.in); got != tt.want {
				t.Fatalf("normalizeBrokerURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
