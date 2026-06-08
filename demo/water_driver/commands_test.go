package water_driver

import "testing"

func TestCommandApplySetsPumpStatus(t *testing.T) {
	network := NewNetwork()
	listener := &commandListener{network: network}

	resp := listener.apply([]byte(`{"id":"cmd-1","WaterWorks.PUMP_STATION.RAW_WATER_PS.pumps.1.status":true}`))
	if !resp.Success {
		t.Fatalf("expected success, got %#v", resp)
	}
	if !network.RawWaterPS.Pumps[1].Running {
		t.Fatal("expected raw water pump 2 to be running")
	}
	if !network.RawWaterPS.Pumps[1].Manual {
		t.Fatal("expected raw water pump 2 to be manually controlled")
	}

	resp = listener.apply([]byte(`{"id":"cmd-2","WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0.status":false}`))
	if !resp.Success {
		t.Fatalf("expected success for new command payload, got %#v", resp)
	}
	if network.NorthBooster.Pumps[0].Running {
		t.Fatal("expected north booster pump 1 to be stopped")
	}
	if !network.NorthBooster.Pumps[0].Manual {
		t.Fatal("expected north booster pump 1 to be manually controlled")
	}
}

func TestCommandApplyClearsPumpManualMode(t *testing.T) {
	network := NewNetwork()
	network.RawWaterPS.Pumps[1].Manual = true
	listener := &commandListener{network: network}

	resp := listener.apply([]byte(`{"id":"cmd-3","WaterWorks.PUMP_STATION.RAW_WATER_PS.pumps.1.manual":false}`))
	if !resp.Success {
		t.Fatalf("expected success, got %#v", resp)
	}
	if network.RawWaterPS.Pumps[1].Manual {
		t.Fatal("expected raw water pump 2 manual mode to be cleared")
	}
}

func TestCommandApplyUsesSubjectStationForRelativePath(t *testing.T) {
	network := NewNetwork()
	listener := &commandListener{network: network}

	resp := listener.applyForSubject(
		"xact.command.default.WaterWorks.BOOSTER_STATION.SOUTH_BOOSTER",
		[]byte(`{"id":"cmd-3","pumps.1.status":true}`),
	)
	if !resp.Success {
		t.Fatalf("expected success, got %#v", resp)
	}
	if !network.SouthBooster.Pumps[1].Running {
		t.Fatal("expected south booster pump 2 to be running")
	}
	if network.RawWaterPS.Pumps[1].Manual {
		t.Fatal("did not expect relative command to fall back to raw water station")
	}
}

func TestCommandApplyUsesSubjectStationForRelativeManualPath(t *testing.T) {
	network := NewNetwork()
	network.SouthBooster.Pumps[1].Manual = true
	listener := &commandListener{network: network}

	resp := listener.applyForSubject(
		"xact.command.default.WaterWorks.BOOSTER_STATION.SOUTH_BOOSTER",
		[]byte(`{"id":"cmd-4","pumps.1.manual":false}`),
	)
	if !resp.Success {
		t.Fatalf("expected success, got %#v", resp)
	}
	if network.SouthBooster.Pumps[1].Manual {
		t.Fatal("expected south booster pump 2 manual mode to be cleared")
	}
	if network.RawWaterPS.Pumps[1].Manual {
		t.Fatal("did not expect relative command to fall back to raw water station")
	}
}

func TestParsePumpStatusPath(t *testing.T) {
	tests := map[string]struct {
		station string
		index   int
	}{
		"pumps.0.status": {station: "RAW_WATER_PS", index: 0},
		"pumps/1/status": {station: "RAW_WATER_PS", index: 1},
		"WaterWorks.PUMP_STATION.RAW_WATER_PS.pumps.0.status":     {station: "RAW_WATER_PS", index: 0},
		"WaterWorks/BOOSTER_STATION/NORTH_BOOSTER/pumps/1/status": {station: "NORTH_BOOSTER", index: 1},
	}

	for path, want := range tests {
		gotStation, gotIndex, ok := parsePumpStatusPath(path, "")
		if !ok || gotStation != want.station || gotIndex != want.index {
			t.Fatalf("parsePumpStatusPath(%q) = %s, %d, %v; want %s, %d, true", path, gotStation, gotIndex, ok, want.station, want.index)
		}
	}
}

func TestParsePumpStatusPathUsesDefaultStation(t *testing.T) {
	gotStation, gotIndex, ok := parsePumpStatusPath("pumps.0.status", "LOW_LIFT_PS")
	if !ok || gotStation != "LOW_LIFT_PS" || gotIndex != 0 {
		t.Fatalf("parsePumpStatusPath with default = %s, %d, %v; want LOW_LIFT_PS, 0, true", gotStation, gotIndex, ok)
	}
}

func TestCommandApplyRejectsUnsupportedPath(t *testing.T) {
	network := NewNetwork()
	listener := &commandListener{network: network}

	resp := listener.apply([]byte(`{"id":"cmd-2","pumps.2.status":true}`))
	if resp.Success {
		t.Fatalf("expected failure, got %#v", resp)
	}
}
