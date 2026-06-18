package lta_incidents_driver

import (
	"context"
	"fmt"
	"log"
	"time"
)

func Start() {
	cfg := ConfigFromEnv()
	fmt.Printf("config %+v\n", cfg)
	log.Printf("[LTA Incidents] starting: lta=%s xact=%s tenant=%s zone=%s poll=%s",
		cfg.LTABaseURL, cfg.XACTBaseURL, cfg.Tenant, cfg.Zone, cfg.PollInterval)

	if err := Run(context.Background(), cfg); err != nil && err != context.Canceled {
		log.Printf("[LTA Incidents] stopped: %v", err)
	}
}

func Run(ctx context.Context, cfg Config) error {
	driver := NewDriverFromConfig(cfg)

	if err := driver.PollOnce(ctx); err != nil {
		log.Printf("[LTA Incidents] poll failed: %v", err)
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := driver.PollOnce(ctx); err != nil {
				log.Printf("[LTA Incidents] poll failed: %v", err)
			}
		}
	}
}
