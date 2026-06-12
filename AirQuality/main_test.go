package main

import (
	"testing"
	"time"
)

func TestBuildDevicesShareDeviceTypeWithVariationSplit(t *testing.T) {
	devices, err := buildDevices(defaultDeviceCount)
	if err != nil {
		t.Fatalf("buildDevices: %v", err)
	}
	if len(devices) != len(laLongBeachIntersections) {
		t.Fatalf("len(devices) = %d, want %d", len(devices), len(laLongBeachIntersections))
	}

	names := map[string]bool{}
	intersections := map[string]bool{}
	var standard, backup int
	for i, d := range devices {
		if names[d.Name] {
			t.Fatalf("duplicate device name %s", d.Name)
		}
		names[d.Name] = true
		if intersections[d.Intersection] {
			t.Fatalf("duplicate intersection %s", d.Intersection)
		}
		intersections[d.Intersection] = true
		if d.Lat == 0 || d.Lon == 0 || d.Intersection == "" {
			t.Fatalf("device missing location: %+v", d)
		}
		anchor := laLongBeachIntersections[i]
		if d.Intersection != anchor.Name || d.Lat != anchor.Lat || d.Lon != anchor.Lon {
			t.Fatalf("device location = %s %.6f %.6f, want %s %.6f %.6f",
				d.Intersection, d.Lat, d.Lon, anchor.Name, anchor.Lat, anchor.Lon)
		}
		if d.Type != airQualityDeviceType {
			t.Fatalf("device type = %s, want %s", d.Type, airQualityDeviceType)
		}
		switch d.Variation {
		case variationStandard:
			standard++
		case variationBackup:
			backup++
		default:
			t.Fatalf("unexpected variation %q", d.Variation)
		}
	}
	if standard != 13 || backup != 12 {
		t.Fatalf("split standard=%d backup=%d, want 13/12", standard, backup)
	}
}

func TestTemplatePayloadBatterySuperset(t *testing.T) {
	standard := buildTemplatePayload(false)
	backup := buildTemplatePayload(true)

	if _, ok := standard["power"]; ok {
		t.Fatal("standard template unexpectedly has power group")
	}
	if _, ok := backup["power"]; !ok {
		t.Fatal("backup template missing power group")
	}

	for _, group := range []string{"meta", "particulate", "gas", "env", "air", "status"} {
		if _, ok := standard[group]; !ok {
			t.Fatalf("standard template missing %s group", group)
		}
		if _, ok := backup[group]; !ok {
			t.Fatalf("backup template missing %s group", group)
		}
	}

	pm25 := standard["particulate"].(map[string]any)["pm25"].(map[string]any)
	if pm25["units"] != "ug/m3" {
		t.Fatalf("pm25 units = %v", pm25["units"])
	}
	if pm25["history"] != true {
		t.Fatal("pm25 history not enabled")
	}
	if _, ok := pm25["limits"].(map[string]any); !ok {
		t.Fatal("pm25 limits missing")
	}
}

func TestTemplatePayloadDoesNotProvisionPersistBlocks(t *testing.T) {
	for name, template := range map[string]payload{
		"standard": buildTemplatePayload(false),
		"backup":   buildTemplatePayload(true),
	} {
		assertNoPersistKey(t, name, template)
	}
}

func assertNoPersistKey(t *testing.T, path string, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v["persist"]; ok {
			t.Fatalf("%s contains persist key", path)
		}
		for key, child := range v {
			assertNoPersistKey(t, path+"."+key, child)
		}
	}
}

func TestTelemetryPayloadBatteryGroupOnlyForBackup(t *testing.T) {
	devices, err := buildDevices(2)
	if err != nil {
		t.Fatalf("buildDevices: %v", err)
	}
	now := time.Date(2026, 5, 31, 9, 30, 0, 0, time.UTC)

	standardPayload := buildTelemetryPayload(devices[0], now)
	if _, ok := standardPayload["power"]; ok {
		t.Fatal("standard telemetry unexpectedly has power group")
	}
	if standardPayload["templateName"] != standardTemplateName {
		t.Fatalf("standard templateName = %v, want %s", standardPayload["templateName"], standardTemplateName)
	}
	if standardPayload["description"] == "" {
		t.Fatal("standard telemetry missing top-level description pseudo tag")
	}
	standardMeta := standardPayload["meta"].(map[string]any)
	if _, ok := standardMeta["description"]; ok {
		t.Fatal("standard telemetry should not provision meta.description")
	}

	backupPayload := buildTelemetryPayload(devices[1], now)
	power, ok := backupPayload["power"].(map[string]any)
	if !ok {
		t.Fatal("backup telemetry missing power group")
	}
	if _, ok := power["batteryVoltage"]; !ok {
		t.Fatal("backup telemetry missing batteryVoltage")
	}
	if backupPayload["templateName"] != backupTemplateName {
		t.Fatalf("templateName = %v, want %s", backupPayload["templateName"], backupTemplateName)
	}
	if backupPayload["description"] == "" {
		t.Fatal("backup telemetry missing top-level description pseudo tag")
	}
	backupMeta := backupPayload["meta"].(map[string]any)
	if _, ok := backupMeta["description"]; ok {
		t.Fatal("backup telemetry should not provision meta.description")
	}
	if backupPayload["ts"].(int64) != now.UnixMilli() {
		t.Fatalf("timestamp = %v, want %d", backupPayload["ts"], now.UnixMilli())
	}
}

func TestBuildDevicesRejectsMoreThanOneDevicePerIntersection(t *testing.T) {
	if _, err := buildDevices(len(laLongBeachIntersections) + 1); err == nil {
		t.Fatal("buildDevices accepted more devices than intersections")
	}
}
