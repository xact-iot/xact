package water_driver

import (
	"log"
	"os"
	"time"
)

// Start runs the Water supply simulation, publishing telemetry to the XACT REST API.
// Configure via environment variables:
//
//	XACT_SERVER_URL   - default http://localhost:8080
//	XACT_API_KEY      - API key for the 'default' organisation
func Start() {
	serverURL := os.Getenv("XACT_SERVER_URL")
	if serverURL == "" {
		serverURL = os.Getenv("XACT_BASE_URL")
	}
	if serverURL == "" {
		serverURL = "http://localhost:8080/xact"
	}
	apiKey := os.Getenv("XACT_API_KEY")
	client := NewClient(serverURL, apiKey)
	network := NewNetwork()
	commands, err := startCommandListener(network)
	if err != nil {
		log.Printf("water_driver: command listener unavailable: %v", err)
	} else {
		defer commands.close()
	}

	log.Printf("water_driver: Town Water Reticulation SCADA simulation started (server=%s org=%s)", serverURL, org)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		network.Step()
		if err := network.Publish(client); err != nil {
			log.Printf("water_driver: publish error: %v", err)
		}
	}
}
