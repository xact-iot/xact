package water_driver

import (
	"encoding/json"
	"math"
	"testing"
)

func TestStepProducesDiagramTelemetry(t *testing.T) {
	network := NewNetwork()
	network.Step()

	if len(network.Districts) != 6 {
		t.Fatalf("district count = %d, want 6", len(network.Districts))
	}
	if len(network.Valves) != 7 {
		t.Fatalf("valve count = %d, want 7", len(network.Valves))
	}
	if network.WTP.RawInflow <= 0 {
		t.Fatalf("WTP raw inflow = %f, want positive", network.WTP.RawInflow)
	}
	if network.MainReservoir.Pressure <= 0 {
		t.Fatalf("main reservoir pressure = %f, want positive", network.MainReservoir.Pressure)
	}
	if network.MainTrunkFlow <= 0 {
		t.Fatalf("main trunk flow = %f, want positive", network.MainTrunkFlow)
	}
	for _, district := range network.Districts {
		if district.Demand <= 0 {
			t.Fatalf("%s demand = %f, want positive", district.ID, district.Demand)
		}
		if district.Pressure <= 0 {
			t.Fatalf("%s pressure = %f, want positive", district.ID, district.Pressure)
		}
	}
}

func TestProvisioningPayloadDescribesPumpManualTag(t *testing.T) {
	network := NewNetwork()
	network.Step()

	var payload map[string]any
	network.postPumpStation(func(_, _ string, p map[string]any) {
		payload = p
	}, func(_, _ float64) map[string]any {
		return map[string]any{}
	}, network.RawWaterPS, "1704067200000")

	pumps, ok := payload["pumps"].(map[string]map[string]any)
	if !ok || len(pumps) == 0 {
		t.Fatalf("pumps payload = %#v, want non-empty map[string]map[string]any", payload["pumps"])
	}

	manual, ok := pumps["0"]["manual"].(map[string]any)
	if !ok {
		t.Fatalf("manual tag = %#v, want expanded tag object", pumps["0"]["manual"])
	}
	desc, _ := manual["description"].(string)
	if desc == "" {
		t.Fatal("manual tag description is empty")
	}
	if desc != "Manual override flag. When 1, an operator command is holding this pump state and automatic lead/lag control will not change it." {
		t.Fatalf("manual tag description = %q", desc)
	}
	if units, _ := manual["units"].(string); units != "state" {
		t.Fatalf("manual tag units = %q, want state", units)
	}
}

func TestProvisioningPayloadConfiguresAnalogHistoryAndUnits(t *testing.T) {
	network := NewNetwork()
	network.Step()

	var payload map[string]any
	network.postStorage(func(_, _ string, p map[string]any) {
		payload = p
	}, func(_, _ float64) map[string]any {
		return map[string]any{}
	}, "DAM", network.UpperDam, "1704067200000")

	tank, ok := payload["tank"].(map[string]any)
	if !ok {
		t.Fatalf("tank payload = %#v, want map[string]any", payload["tank"])
	}
	level, ok := tank["level_m"].(map[string]any)
	if !ok {
		t.Fatalf("level_m tag = %#v, want expanded tag object", tank["level_m"])
	}
	if units, _ := level["units"].(string); units != "m" {
		t.Fatalf("level_m units = %q, want m", units)
	}
	if history, _ := level["history"].(bool); !history {
		t.Fatalf("level_m history = %#v, want true", level["history"])
	}
	wantDeadband := (network.UpperDam.MaxHeightM*0.96 - network.UpperDam.MaxHeightM*0.08) * 0.05
	if deadband, _ := level["deadband"].(float64); math.Abs(deadband-wantDeadband) > 1e-9 {
		t.Fatalf("level_m deadband = %#v, want %v", level["deadband"], wantDeadband)
	}
}

func TestProvisioningPayloadConfiguresPumpStatusHistory(t *testing.T) {
	network := NewNetwork()
	network.Step()

	var payload map[string]any
	network.postPumpStation(func(_, _ string, p map[string]any) {
		payload = p
	}, func(_, _ float64) map[string]any {
		return map[string]any{}
	}, network.RawWaterPS, "1704067200000")

	pumps, ok := payload["pumps"].(map[string]map[string]any)
	if !ok {
		t.Fatalf("pumps payload = %#v, want indexed object map", payload["pumps"])
	}
	status, ok := pumps["0"]["status"].(map[string]any)
	if !ok {
		t.Fatalf("status tag = %#v, want expanded tag object", pumps["0"]["status"])
	}
	if units, _ := status["units"].(string); units != "state" {
		t.Fatalf("status units = %q, want state", units)
	}
	if history, _ := status["history"].(bool); !history {
		t.Fatalf("status history = %#v, want true", status["history"])
	}
}

func TestProvisioningPayloadUsesPumpIndexNodes(t *testing.T) {
	network := NewNetwork()
	network.Step()

	var payload map[string]any
	network.postPumpStation(func(_, _ string, p map[string]any) {
		payload = p
	}, func(_, _ float64) map[string]any {
		return map[string]any{}
	}, network.RawWaterPS, "1704067200000")

	pumps, ok := payload["pumps"].(map[string]map[string]any)
	if !ok {
		t.Fatalf("pumps payload = %#v, want indexed object map", payload["pumps"])
	}
	if _, ok := pumps["0"]["status"]; !ok {
		t.Fatalf("pump 0 status missing from payload %#v", pumps["0"])
	}
	if _, ok := pumps["1"]["status"]; !ok {
		t.Fatalf("pump 1 status missing from payload %#v", pumps["1"])
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	wirePumps, ok := wire["pumps"].(map[string]any)
	if !ok {
		t.Fatalf("wire pumps payload = %#v, want JSON object", wire["pumps"])
	}
	if _, ok := wirePumps["0"].(map[string]any); !ok {
		t.Fatalf("wire pump 0 payload = %#v, want JSON object", wirePumps["0"])
	}
}
