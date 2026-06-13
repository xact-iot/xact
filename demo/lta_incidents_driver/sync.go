package lta_incidents_driver

import (
	"context"
	"errors"
	"fmt"
	"log"
)

type IncidentSource interface {
	FetchTrafficIncidents() ([]TrafficIncident, error)
}

type IncidentSink interface {
	ExistingIncidents(tenant, zone string) (map[string]string, error)
	IngestIncident(tenant, zone string, incident TrafficIncident) error
	DeleteIncidentDevice(tenant, zone, deviceName string) error
	GetIncident(tenant, zone, deviceName string) (TrafficIncident, error)
	CreateIncidentEvent(tenant, zone, deviceName, message string, params map[string]any) error
}

type Driver struct {
	tenant string
	zone   string
	source IncidentSource
	sink   IncidentSink
}

func NewDriver(tenant, zone string, source IncidentSource, sink IncidentSink) *Driver {
	if tenant == "" {
		tenant = DefaultTenant
	}
	if zone == "" {
		zone = DefaultZone
	}
	return &Driver{tenant: tenant, zone: zone, source: source, sink: sink}
}

func NewDriverFromConfig(cfg Config) *Driver {
	ltaHTTPClient := cfg.LTAClient
	xactHTTPClient := cfg.XACTClient
	if cfg.HTTPClient != nil {
		ltaHTTPClient = cfg.HTTPClient
		xactHTTPClient = cfg.HTTPClient
	}
	source := NewLTAClient(cfg.LTAAPIKey, cfg.LTABaseURL, ltaHTTPClient)
	sink := NewXACTClient(cfg.XACTBaseURL, cfg.XACTAPIKey, cfg.XACTUsername, cfg.XACTPassword, xactHTTPClient)
	return NewDriver(cfg.Tenant, cfg.Zone, source, sink)
}

func (d *Driver) PollOnce(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	incidents, err := d.source.FetchTrafficIncidents()
	if err != nil {
		return err
	}
	log.Printf("[LTA Incidents] fetched %d active incidents", len(incidents))

	existing, err := d.sink.ExistingIncidents(d.tenant, d.zone)
	manageLifecycle := true
	if err != nil {
		fmt.Printf("Sink Existing incidents: %v\n", err)
		if !errors.Is(err, ErrXACTLoginUnauthorized) {
			return err
		}
		manageLifecycle = false
		existing = map[string]string{}
		log.Printf("[LTA Incidents] XACT login failed; continuing in ingest-only mode. Set XACT_USERNAME/XACT_PASSWORD to enable incident cleanup and event logs.")
	}

	seen := make(map[string]TrafficIncident, len(incidents))
	for _, incident := range incidents {
		key := incident.CoordinateKey()
		deviceName := incident.DeviceName()
		seen[key] = incident
		seen[deviceName] = incident
		_, alreadyExists := existing[key]
		if !alreadyExists {
			_, alreadyExists = existing[deviceName]
		}

		if err := d.sink.IngestIncident(d.tenant, d.zone, incident); err != nil {
			return fmt.Errorf("ingest incident %s: %w", key, err)
		}
		if manageLifecycle && !alreadyExists {
			if err := d.sink.CreateIncidentEvent(d.tenant, d.zone, deviceName, "LTA incident created: "+incident.Type, map[string]any{
				"message": incident.Message,
			}); err != nil {
				return fmt.Errorf("write created event for %s: %w", key, err)
			}
		}
	}

	if !manageLifecycle {
		return nil
	}

	for key, deviceName := range existing {
		if _, ok := seen[key]; ok {
			continue
		}
		incident, err := d.sink.GetIncident(d.tenant, d.zone, deviceName)
		if err != nil {
			return fmt.Errorf("get resolved incident %s: %w", key, err)
		}
		if err := d.sink.DeleteIncidentDevice(d.tenant, d.zone, deviceName); err != nil {
			return fmt.Errorf("delete resolved incident %s: %w", key, err)
		}
		if err := d.sink.CreateIncidentEvent(d.tenant, d.zone, deviceName, "LTA incident resolved: "+incident.Type, map[string]any{
			"message": incident.Message,
		}); err != nil {
			return fmt.Errorf("write resolved event for %s: %w", key, err)
		}
	}

	return nil
}
