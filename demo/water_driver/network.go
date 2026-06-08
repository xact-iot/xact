package water_driver

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"
)

const (
	org = "default"

	// Timestep: 5 seconds expressed in hours.
	dtHours = 5.0 / 3600.0

	rhoG = 9.81 // kPa per metre of water head.
)

// pumpState holds the physical and operational state of a single pump.
type pumpState struct {
	Running       bool
	Manual        bool
	Fault         bool
	MaxFlowMLH    float64
	MaxHeadM      float64
	RatedKW       float64
	Flow          float64
	SuctionPres   float64
	DischargePres float64
	ElecLoadKW    float64
}

type storageNode struct {
	ID         string
	Name       string
	Lat        float64
	Lon        float64
	MaxVolML   float64
	VolumeML   float64
	MaxHeightM float64
	InletFlow  float64
	OutletFlow float64
	Pressure   float64
}

type pumpStation struct {
	ID                string
	Name              string
	DeviceType        string
	Lat               float64
	Lon               float64
	Pumps             []pumpState
	TotalFlow         float64
	SuctionPressure   float64
	DischargePressure float64
}

type treatmentPlant struct {
	ID                string
	Name              string
	Lat               float64
	Lon               float64
	RawInflow         float64
	TreatedFlow       float64
	OutletFlow        float64
	ClearwellML       float64
	ClearwellMaxML    float64
	TurbidityNTU      float64
	ChlorineMgL       float64
	OutletPressureKPA float64
}

type valveState struct {
	ID          string
	Name        string
	Lat         float64
	Lon         float64
	PositionPct float64
	TargetPct   float64
	Flow        float64
}

type districtState struct {
	ID       string
	Name     string
	Lat      float64
	Lon      float64
	BaseMLH  float64
	Demand   float64
	Flow     float64
	Pressure float64
}

// Network holds the simulation state for the SCADA diagram in
// water_reticulation_scada_diagram.svg.
type Network struct {
	mu sync.Mutex

	UpperDam      storageNode
	LowerDam      storageNode
	MainReservoir storageNode
	WestReservoir storageNode

	RawWaterPS   pumpStation
	LowLiftPS    pumpStation
	NorthBooster pumpStation
	SouthBooster pumpStation
	WestBooster  pumpStation

	WTP treatmentPlant

	Valves    []valveState
	Districts []districtState

	MainTrunkFlow float64
	NorthFeedFlow float64
	EastFeedFlow  float64
	CentralFlow   float64
	SouthFeedFlow float64
	RiversideFlow float64
	WestFeedFlow  float64

	rng         *rand.Rand
	provisioned bool
}

// NewNetwork returns a Network initialised to match the demo SCADA diagram.
func NewNetwork() *Network {
	return &Network{
		UpperDam: storageNode{
			ID: "UPPER_DAM", Name: "Upper Dam", Lat: 40.9210, Lon: -74.4360,
			MaxVolML: 36_000, VolumeML: 32_400, MaxHeightM: 32,
		},
		LowerDam: storageNode{
			ID: "LOWER_DAM", Name: "Lower Dam", Lat: 40.9120, Lon: -74.4365,
			MaxVolML: 28_000, VolumeML: 21_900, MaxHeightM: 26,
		},
		MainReservoir: storageNode{
			ID: "MAIN_RESERVOIR", Name: "Main Reservoir", Lat: 40.9165, Lon: -74.4110,
			MaxVolML: 10_000, VolumeML: 7_650, MaxHeightM: 18,
		},
		WestReservoir: storageNode{
			ID: "WEST_RESERVOIR", Name: "West Reservoir", Lat: 40.9075, Lon: -74.4240,
			MaxVolML: 2_400, VolumeML: 1_650, MaxHeightM: 14,
		},
		RawWaterPS: pumpStation{
			ID: "RAW_WATER_PS", Name: "Raw Water Pump Station", DeviceType: "PUMP_STATION",
			Lat: 40.9200, Lon: -74.4310,
			Pumps: []pumpState{
				{Running: true, MaxFlowMLH: 48, MaxHeadM: 42, RatedKW: 420},
				{Running: false, MaxFlowMLH: 48, MaxHeadM: 42, RatedKW: 420},
			},
		},
		LowLiftPS: pumpStation{
			ID: "LOW_LIFT_PS", Name: "Low Lift Pump Station", DeviceType: "PUMP_STATION",
			Lat: 40.9110, Lon: -74.4310,
			Pumps: []pumpState{
				{Running: true, MaxFlowMLH: 42, MaxHeadM: 36, RatedKW: 360},
				{Running: false, MaxFlowMLH: 42, MaxHeadM: 36, RatedKW: 360},
			},
		},
		NorthBooster: boosterStation("NORTH_BOOSTER", "North Booster Station", 40.9190, -74.4040, 34, 52),
		SouthBooster: boosterStation("SOUTH_BOOSTER", "South Booster Station", 40.9035, -74.4055, 44, 48),
		WestBooster:  boosterStation("WEST_BOOSTER", "West Booster Station", 40.9045, -74.4300, 29, 44),
		WTP: treatmentPlant{
			ID: "WTP_01", Name: "Water Treatment Plant", Lat: 40.9160, Lon: -74.4210,
			ClearwellML: 118, ClearwellMaxML: 180, TurbidityNTU: 0.18, ChlorineMgL: 1.05,
		},
		Valves: []valveState{
			{ID: "WTP_OUTLET_VLV", Name: "WTP Outlet Valve", Lat: 40.9162, Lon: -74.4140, PositionPct: 100, TargetPct: 100},
			{ID: "WEST_RES_FEED_VLV", Name: "West Reservoir Feed Valve", Lat: 40.9080, Lon: -74.4180, PositionPct: 67, TargetPct: 67},
			{ID: "WESTSIDE_ISO_VLV", Name: "Westside Isolation Valve", Lat: 40.9046, Lon: -74.4320, PositionPct: 74, TargetPct: 74},
			{ID: "NORTH_FEED_VLV", Name: "North Feed Valve", Lat: 40.9205, Lon: -74.4100, PositionPct: 82, TargetPct: 82},
			{ID: "NORTH_HILLS_ISO_VLV", Name: "North Hills Isolation Valve", Lat: 40.9190, Lon: -74.3980, PositionPct: 88, TargetPct: 88},
			{ID: "SOUTH_FEED_VLV", Name: "South Feed Valve", Lat: 40.9040, Lon: -74.4110, PositionPct: 86, TargetPct: 86},
			{ID: "SOUTHTOWN_ISO_VLV", Name: "Southtown Isolation Valve", Lat: 40.9035, Lon: -74.3985, PositionPct: 91, TargetPct: 91},
		},
		Districts: []districtState{
			{ID: "WESTSIDE", Name: "Westside", Lat: 40.9047, Lon: -74.4350, BaseMLH: 26.7},
			{ID: "NORTH_HILLS", Name: "North Hills", Lat: 40.9195, Lon: -74.3965, BaseMLH: 32.6},
			{ID: "EASTVALE", Name: "Eastvale", Lat: 40.9140, Lon: -74.3970, BaseMLH: 28.4},
			{ID: "CENTRAL_HEIGHTS", Name: "Central Heights", Lat: 40.9095, Lon: -74.3975, BaseMLH: 74.8},
			{ID: "SOUTHTOWN", Name: "Southtown", Lat: 40.9035, Lon: -74.3970, BaseMLH: 41.2},
			{ID: "RIVERSIDE", Name: "Riverside", Lat: 40.9015, Lon: -74.3970, BaseMLH: 50.8},
		},
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func boosterStation(id, name string, lat, lon, maxFlow, head float64) pumpStation {
	return pumpStation{
		ID: id, Name: name, DeviceType: "BOOSTER_STATION", Lat: lat, Lon: lon,
		Pumps: []pumpState{
			{Running: true, MaxFlowMLH: maxFlow, MaxHeadM: head, RatedKW: maxFlow * 8.5},
			{Running: false, MaxFlowMLH: maxFlow, MaxHeadM: head, RatedKW: maxFlow * 8.5},
		},
	}
}

func (n *Network) noise(scale float64) float64 {
	return (n.rng.Float64()*2 - 1) * scale
}

// demandFactor returns 0-1 based on typical municipal diurnal demand.
func demandFactor() float64 {
	now := time.Now()
	h := float64(now.Hour()) + float64(now.Minute())/60.0 + float64(now.Second())/3600.0
	morning := math.Exp(-0.5 * math.Pow((h-8.0)/1.45, 2))
	evening := math.Exp(-0.5 * math.Pow((h-18.5)/1.6, 2))
	overnight := 0.35 + 0.15*math.Sin((h-2.0)/24.0*2*math.Pi)
	return clamp(overnight+0.65*math.Max(morning, evening), 0.32, 1.08)
}

// Step advances the simulation by one 5-second timestep.
func (n *Network) Step() {
	n.mu.Lock()
	defer n.mu.Unlock()

	df := demandFactor()
	n.autoControl(df)

	for i := range n.Valves {
		n.Valves[i].PositionPct = clamp(n.Valves[i].PositionPct+(n.Valves[i].TargetPct-n.Valves[i].PositionPct)*0.08+n.noise(0.08), 0, 100)
	}

	upperPressure := storagePressure(n.UpperDam, 265)
	lowerPressure := storagePressure(n.LowerDam, 230)

	upperFlow := n.stationCapacityFlow(&n.RawWaterPS, upperPressure, 0.86+0.14*df)
	lowerFlow := n.stationCapacityFlow(&n.LowLiftPS, lowerPressure, 0.84+0.16*df)

	n.UpperDam.OutletFlow = limitedByStorage(upperFlow, n.UpperDam.VolumeML)
	n.LowerDam.OutletFlow = limitedByStorage(lowerFlow, n.LowerDam.VolumeML)
	n.updateStationReadings(&n.RawWaterPS, n.UpperDam.OutletFlow, upperPressure, upperPressure+headBoost(&n.RawWaterPS))
	n.updateStationReadings(&n.LowLiftPS, n.LowerDam.OutletFlow, lowerPressure, lowerPressure+headBoost(&n.LowLiftPS))

	n.WTP.RawInflow = n.UpperDam.OutletFlow + n.LowerDam.OutletFlow
	treatmentRecovery := 0.982 + n.noise(0.003)
	n.WTP.TreatedFlow = clamp(n.WTP.RawInflow*treatmentRecovery, 0, 125)

	mainPct := levelPct(n.MainReservoir)
	reservoirRefillBias := clamp((82-mainPct)*0.9, -8, 22)
	desiredWTPOut := clamp(n.WTP.TreatedFlow+reservoirRefillBias, 30, 132)
	wtpValve := n.valveFactor("WTP_OUTLET_VLV")
	clearwellAvailable := n.WTP.ClearwellML / dtHours
	n.WTP.OutletFlow = clamp(math.Min(desiredWTPOut*wtpValve, n.WTP.TreatedFlow+clearwellAvailable), 0, 140)
	n.WTP.ClearwellML = clamp(n.WTP.ClearwellML+(n.WTP.TreatedFlow-n.WTP.OutletFlow)*dtHours, 22, n.WTP.ClearwellMaxML)
	n.WTP.TurbidityNTU = clamp(0.17+0.04*(1-df)+n.noise(0.015), 0.04, 0.55)
	n.WTP.ChlorineMgL = clamp(1.02+0.12*df+n.noise(0.03), 0.65, 1.65)

	mainPressure := storagePressure(n.MainReservoir, 430)
	n.WTP.OutletPressureKPA = mainPressure + 28 + n.noise(2)

	for i := range n.Districts {
		randomCustomerLoad := 1.0 + n.noise(0.045)
		n.Districts[i].Demand = clamp(n.Districts[i].BaseMLH*df*randomCustomerLoad, n.Districts[i].BaseMLH*0.25, n.Districts[i].BaseMLH*1.15)
	}

	westFeedDemand := clamp((72-levelPct(n.WestReservoir))*1.2+24*df, 6, 48)
	n.WestFeedFlow = valveLimitedFlow(westFeedDemand, n.valveFactor("WEST_RES_FEED_VLV"), mainPressure, 410)

	westPressure := storagePressure(n.WestReservoir, 365)
	n.WestFeedFlow = math.Min(n.WestFeedFlow, limitedByStorage(n.MainReservoir.VolumeML/dtHours, n.MainReservoir.VolumeML))
	n.updateDistrictFromBooster(0, &n.WestBooster, westPressure, 44, "WESTSIDE_ISO_VLV")
	n.updateDistrictFromBooster(1, &n.NorthBooster, mainPressure-40, 68, "NORTH_FEED_VLV", "NORTH_HILLS_ISO_VLV")
	n.updateDirectDistrict(2, mainPressure-55, 52)
	n.updateDirectDistrict(3, mainPressure-78, 96)
	n.updateDistrictFromBooster(4, &n.SouthBooster, mainPressure-82, 58, "SOUTH_FEED_VLV", "SOUTHTOWN_ISO_VLV")
	n.updateDirectDistrict(5, mainPressure-118, 68)

	n.NorthFeedFlow = n.Districts[1].Flow
	n.EastFeedFlow = n.Districts[2].Flow
	n.CentralFlow = n.Districts[3].Flow
	n.SouthFeedFlow = n.Districts[4].Flow
	n.RiversideFlow = n.Districts[5].Flow
	n.MainTrunkFlow = n.NorthFeedFlow + n.EastFeedFlow + n.CentralFlow + n.SouthFeedFlow + n.RiversideFlow + n.WestFeedFlow

	n.setValveFlow("WTP_OUTLET_VLV", n.WTP.OutletFlow)
	n.setValveFlow("WEST_RES_FEED_VLV", n.WestFeedFlow)
	n.setValveFlow("WESTSIDE_ISO_VLV", n.Districts[0].Flow)
	n.setValveFlow("NORTH_FEED_VLV", n.NorthFeedFlow)
	n.setValveFlow("NORTH_HILLS_ISO_VLV", n.Districts[1].Flow)
	n.setValveFlow("SOUTH_FEED_VLV", n.SouthFeedFlow)
	n.setValveFlow("SOUTHTOWN_ISO_VLV", n.Districts[4].Flow)

	n.UpperDam.InletFlow = 18 + 3*math.Sin(float64(time.Now().Unix()%3600)/3600.0*2*math.Pi) + n.noise(1.5)
	n.LowerDam.InletFlow = 14 + 2*math.Sin(float64(time.Now().Unix()%5400)/5400.0*2*math.Pi) + n.noise(1.2)
	n.MainReservoir.InletFlow = n.WTP.OutletFlow
	n.MainReservoir.OutletFlow = n.MainTrunkFlow
	n.WestReservoir.InletFlow = n.WestFeedFlow
	n.WestReservoir.OutletFlow = n.Districts[0].Flow

	n.UpperDam.VolumeML = clamp(n.UpperDam.VolumeML+(n.UpperDam.InletFlow-n.UpperDam.OutletFlow)*dtHours, n.UpperDam.MaxVolML*0.18, n.UpperDam.MaxVolML)
	n.LowerDam.VolumeML = clamp(n.LowerDam.VolumeML+(n.LowerDam.InletFlow-n.LowerDam.OutletFlow)*dtHours, n.LowerDam.MaxVolML*0.15, n.LowerDam.MaxVolML)
	n.MainReservoir.VolumeML = clamp(n.MainReservoir.VolumeML+(n.MainReservoir.InletFlow-n.MainReservoir.OutletFlow)*dtHours, n.MainReservoir.MaxVolML*0.08, n.MainReservoir.MaxVolML)
	n.WestReservoir.VolumeML = clamp(n.WestReservoir.VolumeML+(n.WestReservoir.InletFlow-n.WestReservoir.OutletFlow)*dtHours, n.WestReservoir.MaxVolML*0.10, n.WestReservoir.MaxVolML)

	n.UpperDam.Pressure = storagePressure(n.UpperDam, 265)
	n.LowerDam.Pressure = storagePressure(n.LowerDam, 230)
	n.MainReservoir.Pressure = storagePressure(n.MainReservoir, 430)
	n.WestReservoir.Pressure = storagePressure(n.WestReservoir, 365)
}

func (n *Network) autoControl(df float64) {
	mainPct := levelPct(n.MainReservoir)
	westPct := levelPct(n.WestReservoir)

	n.controlLeadLag(&n.RawWaterPS, mainPct, 76, 90, df > 0.82)
	n.controlLeadLag(&n.LowLiftPS, mainPct, 74, 88, df > 0.80)
	n.controlLeadLag(&n.WestBooster, westPct, 40, 92, df > 0.72)
	n.controlLeadLag(&n.NorthBooster, mainPct, 42, 96, df > 0.75)
	n.controlLeadLag(&n.SouthBooster, mainPct, 42, 96, df > 0.78)
}

func (n *Network) controlLeadLag(ps *pumpStation, level, startBelow, stopAbove float64, highDemand bool) {
	if len(ps.Pumps) == 0 {
		return
	}
	if !ps.Pumps[0].Manual {
		if level < startBelow || highDemand {
			ps.Pumps[0].Running = true
		}
		if level > stopAbove && !highDemand {
			ps.Pumps[0].Running = false
		}
	}
	if len(ps.Pumps) < 2 || ps.Pumps[1].Manual {
		return
	}
	if level < startBelow-8 || highDemand && level < stopAbove-4 {
		ps.Pumps[1].Running = true
	}
	if level > startBelow+10 && !highDemand {
		ps.Pumps[1].Running = false
	}
	if ps.Pumps[0].Running && !ps.Pumps[0].Fault && n.rng.Float64() < 0.00018 {
		ps.Pumps[0].Running = false
		ps.Pumps[0].Fault = true
		ps.Pumps[1].Running = true
		log.Printf("water: %s lead pump tripped; lag pump started", ps.ID)
	}
	if ps.Pumps[0].Fault && n.rng.Float64() < 0.003 {
		ps.Pumps[0].Fault = false
		log.Printf("water: %s lead pump fault cleared", ps.ID)
	}
}

func (n *Network) stationCapacityFlow(ps *pumpStation, suctionPressure, dutyFactor float64) float64 {
	total := 0.0
	for i := range ps.Pumps {
		if !ps.Pumps[i].Running || ps.Pumps[i].Fault {
			continue
		}
		suctionFactor := clamp(suctionPressure/480, 0.55, 1.08)
		total += ps.Pumps[i].MaxFlowMLH * dutyFactor * suctionFactor
	}
	return math.Max(0, total+n.noise(math.Max(0.3, total*0.025)))
}

func (n *Network) updateDirectDistrict(index int, inletPressure, branchCapacity float64) {
	d := &n.Districts[index]
	pressureFactor := clamp((inletPressure-250)/280, 0.20, 1.08)
	d.Flow = clamp(math.Min(d.Demand, branchCapacity)*pressureFactor+n.noise(0.25), 0, d.Demand*1.03)
	d.Pressure = clamp(inletPressure-0.65*d.Flow+n.noise(2.4), 90, 680)
}

func (n *Network) updateDistrictFromBooster(index int, ps *pumpStation, suctionPressure float64, branchCapacity float64, valveIDs ...string) {
	d := &n.Districts[index]
	available := n.stationCapacityFlow(ps, suctionPressure, 0.86+0.10*d.Demand/math.Max(1, d.BaseMLH))
	available = math.Min(available, branchCapacity)
	valveFactor := 1.0
	for _, id := range valveIDs {
		valveFactor *= n.valveFactor(id)
	}
	flow := valveLimitedFlow(math.Min(d.Demand, available), valveFactor, suctionPressure, 340)
	discharge := suctionPressure + headBoost(ps) - 0.45*flow + n.noise(2)
	if flow < 0.1 {
		discharge = suctionPressure
	}
	d.Flow = clamp(flow+n.noise(0.18), 0, d.Demand*1.04)
	d.Pressure = clamp(discharge, 90, 720)
	n.updateStationReadings(ps, d.Flow, suctionPressure, discharge)
}

func (n *Network) updateStationReadings(ps *pumpStation, totalFlow, suctionPressure, dischargePressure float64) {
	ps.TotalFlow = math.Max(0, totalFlow)
	ps.SuctionPressure = suctionPressure
	ps.DischargePressure = dischargePressure

	active := 0
	for _, p := range ps.Pumps {
		if p.Running && !p.Fault {
			active++
		}
	}
	for i := range ps.Pumps {
		p := &ps.Pumps[i]
		p.SuctionPres = suctionPressure
		if p.Running && !p.Fault && active > 0 {
			p.Flow = totalFlow / float64(active)
			p.DischargePres = dischargePressure
			loadFactor := clamp(p.Flow/math.Max(1, p.MaxFlowMLH), 0.2, 1.18)
			p.ElecLoadKW = clamp(p.RatedKW*loadFactor+n.noise(p.RatedKW*0.025), 0, p.RatedKW*1.25)
		} else {
			p.Flow = 0
			p.DischargePres = suctionPressure
			p.ElecLoadKW = 0
		}
	}
}

// Publish sends all device telemetry to the XACT REST API.
// On the first call payloads include units, deadbands and limits so the server
// auto-provisions each tag pipeline. Later calls send only values.
func (n *Network) Publish(c *Client) error {
	n.mu.Lock()
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())

	type pendingPost struct {
		deviceType string
		deviceName string
		payload    map[string]any
	}
	var posts []pendingPost
	post := func(deviceType, deviceName string, payload map[string]any) {
		posts = append(posts, pendingPost{deviceType: deviceType, deviceName: deviceName, payload: payload})
	}
	meta := func(lat, lon float64) map[string]any {
		if n.provisioned {
			return map[string]any{"lat": lat, "lon": lon, "ts": ts, "online": true}
		}
		return map[string]any{
			"lat":    n.describedTagWithUnits(lat, "deg", "Site latitude in decimal degrees."),
			"lon":    n.describedTagWithUnits(lon, "deg", "Site longitude in decimal degrees."),
			"online": n.discreteTag(true, "state", "True when the simulated device is publishing current telemetry.", false),
			"ts":     ts,
		}
	}

	n.postStorage(post, meta, "DAM", n.UpperDam, ts)
	n.postStorage(post, meta, "DAM", n.LowerDam, ts)
	n.postPumpStation(post, meta, n.RawWaterPS, ts)
	n.postPumpStation(post, meta, n.LowLiftPS, ts)
	n.postTreatmentPlant(post, meta, ts)
	n.postStorage(post, meta, "RESERVOIR", n.MainReservoir, ts)
	n.postStorage(post, meta, "RESERVOIR", n.WestReservoir, ts)
	n.postPumpStation(post, meta, n.NorthBooster, ts)
	n.postPumpStation(post, meta, n.SouthBooster, ts)
	n.postPumpStation(post, meta, n.WestBooster, ts)
	n.postDistricts(post, meta, ts)
	n.postValves(post, meta, ts)

	n.provisioned = true
	n.mu.Unlock()

	var firstErr error
	for _, p := range posts {
		if err := c.Post(p.deviceType, p.deviceName, p.payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (n *Network) postStorage(post func(string, string, map[string]any), meta func(float64, float64) map[string]any, deviceType string, s storageNode, ts string) {
	post(deviceType, s.ID, map[string]any{
		"meta": meta(s.Lat, s.Lon),
		"tank": map[string]any{
			"level_m":   n.tag(s.VolumeML/s.MaxVolML*s.MaxHeightM, "m", s.MaxHeightM*0.96, s.MaxHeightM*0.08, "Water height above the storage floor."),
			"level_pct": n.tag(levelPct(s), "%", 95, 15, "Storage fill level as a percentage of usable capacity."),
			"volume_ml": n.tag(s.VolumeML, "ML", s.MaxVolML*0.96, s.MaxVolML*0.08, "Current stored water volume."),
			"ts":        ts,
		},
		"sensors": map[string]any{
			"inlet_flow_mlh":      n.tag(s.InletFlow, "ML/h", s.MaxVolML*0.03, 0, "Flow entering the storage asset."),
			"outlet_flow_mlh":     n.tag(s.OutletFlow, "ML/h", s.MaxVolML*0.03, 0, "Flow leaving the storage asset."),
			"outlet_pressure_kpa": n.tag(s.Pressure, "kPa", 700, 80, "Water pressure at the storage outlet."),
			"ts":                  ts,
		},
	})
}

func (n *Network) postPumpStation(post func(string, string, map[string]any), meta func(float64, float64) map[string]any, ps pumpStation, ts string) {
	pumps := make(map[string]map[string]any, len(ps.Pumps))
	for i, p := range ps.Pumps {
		pumps[fmt.Sprintf("%d", i)] = map[string]any{
			"status":             n.discreteTag(boolToInt(p.Running && !p.Fault), "state", "1 when the pump is running and healthy; 0 when stopped or faulted.", true),
			"fault":              n.discreteTag(boolToInt(p.Fault), "state", "1 when the pump has tripped or is unavailable; 0 when available.", false),
			"manual":             n.discreteTag(boolToInt(p.Manual), "state", "Manual override flag. When 1, an operator command is holding this pump state and automatic lead/lag control will not change it.", false),
			"flow_mlh":           n.tag(p.Flow, "ML/h", p.MaxFlowMLH*1.2, 0, "Flow currently delivered by this pump."),
			"suction_pres_kpa":   n.tag(p.SuctionPres, "kPa", 700, 60, "Pressure measured at this pump's suction side."),
			"discharge_pres_kpa": n.tag(p.DischargePres, "kPa", 820, 80, "Pressure measured at this pump's discharge side."),
			"elec_load_kw":       n.tag(p.ElecLoadKW, "kW", p.RatedKW*1.18, 0, "Estimated electrical load drawn by this pump."),
		}
	}
	post(ps.DeviceType, ps.ID, map[string]any{
		"meta": meta(ps.Lat, ps.Lon),
		"sensors": map[string]any{
			"total_flow_mlh":       n.tag(ps.TotalFlow, "ML/h", 180, 0, "Combined flow delivered by all running pumps in this station."),
			"suction_pres_kpa":     n.tag(ps.SuctionPressure, "kPa", 700, 60, "Common suction header pressure for the station."),
			"discharge_pres_kpa":   n.tag(ps.DischargePressure, "kPa", 820, 80, "Common discharge header pressure for the station."),
			"running_pump_count":   n.tag(float64(runningPumpCount(ps)), "count", float64(len(ps.Pumps)), 0, "Number of pumps currently running without a fault."),
			"available_pump_count": n.tag(float64(availablePumpCount(ps)), "count", float64(len(ps.Pumps)), 0, "Number of pumps not currently faulted."),
			"ts":                   ts,
		},
		"pumps": pumps,
	})
}

func (n *Network) postTreatmentPlant(post func(string, string, map[string]any), meta func(float64, float64) map[string]any, ts string) {
	post("TREATMENT_PLANT", n.WTP.ID, map[string]any{
		"meta": meta(n.WTP.Lat, n.WTP.Lon),
		"sensors": map[string]any{
			"raw_inflow_mlh":      n.tag(n.WTP.RawInflow, "ML/h", 150, 0, "Untreated water entering the treatment plant."),
			"treated_flow_mlh":    n.tag(n.WTP.TreatedFlow, "ML/h", 145, 0, "Water flow leaving the treatment process before clearwell storage."),
			"outlet_flow_mlh":     n.tag(n.WTP.OutletFlow, "ML/h", 145, 0, "Treated water delivered from the plant to the distribution system."),
			"clearwell_level_pct": n.tag(n.WTP.ClearwellML/n.WTP.ClearwellMaxML*100, "%", 95, 12, "Clearwell storage level as a percentage of capacity."),
			"turbidity_ntu":       n.tag(n.WTP.TurbidityNTU, "NTU", 0.5, 0, "Treated water turbidity measurement."),
			"chlorine_mgl":        n.tag(n.WTP.ChlorineMgL, "mg/L", 1.55, 0.65, "Free chlorine residual in treated water."),
			"outlet_pressure_kpa": n.tag(n.WTP.OutletPressureKPA, "kPa", 720, 180, "Pressure at the treatment plant outlet header."),
			"ts":                  ts,
		},
	})
}

func (n *Network) postDistricts(post func(string, string, map[string]any), meta func(float64, float64) map[string]any, ts string) {
	for _, d := range n.Districts {
		post("DISTRICT", d.ID, map[string]any{
			"meta": meta(d.Lat, d.Lon),
			"sensors": map[string]any{
				"demand_mlh":        n.tag(d.Demand, "ML/h", d.BaseMLH*1.3, 0, "Estimated customer demand for this district."),
				"supplied_flow_mlh": n.tag(d.Flow, "ML/h", d.BaseMLH*1.3, 0, "Water flow currently supplied to this district."),
				"pressure_kpa":      n.tag(d.Pressure, "kPa", 720, 180, "Representative distribution pressure in this district."),
				"service_pct":       n.tag(clamp(d.Flow/math.Max(1, d.Demand)*100, 0, 120), "%", 110, 80, "Supplied flow as a percentage of current district demand."),
				"ts":                ts,
			},
		})
	}
}

func (n *Network) postValves(post func(string, string, map[string]any), meta func(float64, float64) map[string]any, ts string) {
	for _, v := range n.Valves {
		post("VALVE", v.ID, map[string]any{
			"meta": meta(v.Lat, v.Lon),
			"sensors": map[string]any{
				"position_pct": n.tag(v.PositionPct, "%", 100, 0, "Valve opening position, where 0 is closed and 100 is fully open."),
				"flow_mlh":     n.tag(v.Flow, "ML/h", 150, 0, "Flow currently passing through this valve."),
				"open_status":  n.discreteTag(boolToInt(v.PositionPct > 5), "state", "1 when the valve is more than 5 percent open; 0 when effectively closed.", false),
				"ts":           ts,
			},
		})
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SetPumpStatus applies a command to a pump in any station shown on the SVG.
func (n *Network) SetPumpStatus(stationID string, index int, running bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ps := n.stationByIDLocked(stationID)
	if ps == nil {
		return fmt.Errorf("station %s not found", stationID)
	}
	if index < 0 || index >= len(ps.Pumps) {
		return fmt.Errorf("pump index %d out of range for %s", index, ps.ID)
	}

	ps.Pumps[index].Running = running
	ps.Pumps[index].Manual = true
	if running {
		ps.Pumps[index].Fault = false
	}
	log.Printf("water: %s Pump%d commanded %s", ps.ID, index+1, onOff(running))
	return nil
}

// SetPumpManual applies a command to a pump manual override flag.
func (n *Network) SetPumpManual(stationID string, index int, manual bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ps := n.stationByIDLocked(stationID)
	if ps == nil {
		return fmt.Errorf("station %s not found", stationID)
	}
	if index < 0 || index >= len(ps.Pumps) {
		return fmt.Errorf("pump index %d out of range for %s", index, ps.ID)
	}

	ps.Pumps[index].Manual = manual
	log.Printf("water: %s Pump%d manual mode %s", ps.ID, index+1, manualState(manual))
	return nil
}

// SetPS01PumpStatus is kept as a compatibility shim for old command tests and
// schedulers. It maps the former two-pump station to the raw water station.
func (n *Network) SetPS01PumpStatus(index int, running bool) error {
	return n.SetPumpStatus("RAW_WATER_PS", index, running)
}

func (n *Network) stationByIDLocked(id string) *pumpStation {
	switch normalizeStationID(id) {
	case "RAW_WATER_PS", "RAW_WATER_PUMP_STATION", "RWPS_01", "PS_01":
		return &n.RawWaterPS
	case "LOW_LIFT_PS", "LOW_LIFT_PUMP_STATION", "LLPS_01":
		return &n.LowLiftPS
	case "NORTH_BOOSTER", "NORTH_BOOSTER_STATION", "NB_01":
		return &n.NorthBooster
	case "SOUTH_BOOSTER", "SOUTH_BOOSTER_STATION", "SB_01":
		return &n.SouthBooster
	case "WEST_BOOSTER", "WEST_BOOSTER_STATION", "WB_01":
		return &n.WestBooster
	default:
		return nil
	}
}

func normalizeStationID(id string) string {
	return strings.ToUpper(strings.TrimSpace(id))
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func manualState(v bool) string {
	if v {
		return "set"
	}
	return "cleared"
}

// tag returns a tag object for an analog value.
func (n *Network) tag(value float64, units string, hi, lo float64, description string) map[string]any {
	if n.provisioned {
		return map[string]any{"value": value}
	}
	return map[string]any{
		"value":       value,
		"description": description,
		"units":       units,
		"deadband":    analogDeadband(hi, lo),
		"history":     true,
		"limits":      map[string]any{"hi": hi, "lo": lo},
	}
}

func analogDeadband(hi, lo float64) float64 {
	return math.Abs(hi-lo) * 0.05
}

// describedTag returns a tag object for discrete or non-analog values.
func (n *Network) describedTag(value any, description string) any {
	return n.describedTagWithUnits(value, "", description)
}

func (n *Network) describedTagWithUnits(value any, units, description string) any {
	if n.provisioned {
		return value
	}
	tag := map[string]any{
		"value":       value,
		"description": description,
	}
	if units != "" {
		tag["units"] = units
	}
	return tag
}

func (n *Network) discreteTag(value any, units, description string, history bool) any {
	tag := n.describedTagWithUnits(value, units, description)
	if n.provisioned || !history {
		return tag
	}
	tag.(map[string]any)["history"] = true
	return tag
}

func levelPct(s storageNode) float64 {
	return s.VolumeML / s.MaxVolML * 100
}

func storagePressure(s storageNode, staticLift float64) float64 {
	head := s.VolumeML / s.MaxVolML * s.MaxHeightM
	return staticLift + rhoG*head
}

func limitedByStorage(flow, volumeML float64) float64 {
	return math.Min(flow, math.Max(0, volumeML*0.015/dtHours))
}

func valveLimitedFlow(demand, valveFactor, pressure, minPressure float64) float64 {
	pressureFactor := clamp((pressure-minPressure)/180, 0.25, 1.08)
	return clamp(demand*valveFactor*pressureFactor, 0, demand*1.05)
}

func headBoost(ps *pumpStation) float64 {
	active := 0
	totalHead := 0.0
	for _, p := range ps.Pumps {
		if p.Running && !p.Fault {
			active++
			totalHead += p.MaxHeadM
		}
	}
	if active == 0 {
		return 0
	}
	return rhoG * (totalHead / float64(active)) * 0.82
}

func runningPumpCount(ps pumpStation) int {
	count := 0
	for _, p := range ps.Pumps {
		if p.Running && !p.Fault {
			count++
		}
	}
	return count
}

func availablePumpCount(ps pumpStation) int {
	count := 0
	for _, p := range ps.Pumps {
		if !p.Fault {
			count++
		}
	}
	return count
}

func (n *Network) valveFactor(id string) float64 {
	for i := range n.Valves {
		if n.Valves[i].ID == id {
			return clamp(n.Valves[i].PositionPct/100, 0, 1)
		}
	}
	return 1
}

func (n *Network) setValveFlow(id string, flow float64) {
	for i := range n.Valves {
		if n.Valves[i].ID == id {
			n.Valves[i].Flow = math.Max(0, flow)
			return
		}
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
