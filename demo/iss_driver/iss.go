package iss_driver

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	serverURL                = "https://push.lightstreamer.com/lightstreamer"
	issOEMURL                = "https://nasa-public-data.s3.amazonaws.com/iss-coords/current/ISS_OEM/ISS.OEM_J2K_EPH.txt"
	adapterSet               = "ISSLIVE"
	offlineTimeout           = 10 * time.Minute
	lightstreamerIdleTimeout = 5 * time.Minute
	snapshotTimeout          = 30 * time.Second
	snapshotSchema           = "TimeStamp Value Status.Class Status.Indicator Status.Color CalibratedData"
	orbitPublish             = 5 * time.Second
	orbitRefresh             = 30 * time.Minute
)

type telemetryItem struct {
	itemID     string
	label      string
	unit       string
	enumValues map[int]string
	history    bool
}

type lightstreamerSession struct {
	id     string
	reader *bufio.Reader
	body   io.Closer
}

type ephemerisPoint struct {
	at  time.Time
	pos [3]float64
	vel [3]float64
}

// items maps Lightstreamer table ID (1-based) to telemetry definition.
// Tables 1-6 are the ISSLIVE J2000 state vector, but live map position is
// published from NASA's timestamped OEM ephemeris instead.
var items = []telemetryItem{
	{itemID: "USLAB000032", label: "J2000 Position X", unit: "km"},
	{itemID: "USLAB000033", label: "J2000 Position Y", unit: "km"},
	{itemID: "USLAB000034", label: "J2000 Position Z", unit: "km"},
	{itemID: "USLAB000035", label: "Velocity X", unit: "m/s"},
	{itemID: "USLAB000036", label: "Velocity Y", unit: "m/s"},
	{itemID: "USLAB000037", label: "Velocity Z", unit: "m/s"},
	{itemID: "USLAB000053", label: "O2 Partial Pressure", unit: "mmHg", history: true},
	{itemID: "USLAB000055", label: "CO2 Partial Pressure", unit: "mmHg", history: true},
	{itemID: "USLAB000058", label: "Cabin Pressure", unit: "torr", history: true},
	{itemID: "USLAB000059", label: "Cabin Temperature", unit: "°C", history: true},
	{itemID: "NODE3000005", label: "Urine Tank", unit: "%", history: true},
	{itemID: "NODE3000009", label: "Clean Water Tank", unit: "%", history: true},
	{itemID: "NODE3000010", label: "O2 Generator State", unit: "", enumValues: map[int]string{
		1: "PROCESS",
		2: "STANDBY",
		3: "SHUTDOWN",
		4: "STOP",
		5: "VENT_DOME",
		6: "INERT_DOME",
		7: "FAST_SHUTDOWN",
		8: "N2_PURGE_SHUTDOWN",
	}},
	{itemID: "NODE3000004", label: "Urine Processor State", unit: "", enumValues: map[int]string{
		2:   "STOP",
		4:   "SHUTDOWN",
		8:   "MAINTENANCE",
		16:  "NORMAL",
		32:  "STANDBY",
		64:  "IDLE",
		128: "SYSTEM INITIALIZED",
	}},
	{itemID: "USLAB000001", label: "CMG 1 Status", unit: "", enumValues: map[int]string{
		0: "NOT IN USE",
		1: "IN USE",
	}},
	{itemID: "USLAB000012", label: "GNC Mode", unit: "", enumValues: map[int]string{
		0: "Default",
		1: "WAIT",
		2: "RESERVED",
		3: "STANDBY",
		4: "CMG ATTITUDE CONTROL",
		5: "CMG/THRUSTER ASSIST ATTITUDE CONTROL",
		6: "USER DATA GENERATION",
		7: "FREEDRIFT",
	}},
}

// julianDate converts a UTC time to Julian Date.
func julianDate(t time.Time) float64 {
	return float64(t.Unix())/86400.0 + 2440587.5
}

// gmst returns the Greenwich Mean Sidereal Time in radians for the given UTC time.
func gmst(t time.Time) float64 {
	jd := julianDate(t)
	T := (jd - 2451545.0) / 36525.0
	deg := 280.46061837 + 360.98564736629*(jd-2451545.0) +
		T*T*(0.000387933-T/38710000.0)
	deg = math.Mod(deg, 360.0)
	if deg < 0 {
		deg += 360.0
	}
	return deg * math.Pi / 180.0
}

// eciToLatLonAlt converts J2000 ECI position (km) to geodetic lat/lon (degrees)
// and altitude (km) using the position sample time for the sidereal rotation.
func eciToLatLonAlt(x, y, z float64, at time.Time) (lat, lon, alt float64) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	theta := gmst(at.UTC())

	// Rotate ECI → ECEF
	ex := x*math.Cos(theta) + y*math.Sin(theta)
	ey := -x*math.Sin(theta) + y*math.Cos(theta)
	ez := z

	// ECEF → geodetic (spherical approximation; error < 0.2°)
	lon = math.Atan2(ey, ex) * 180.0 / math.Pi
	r := math.Sqrt(ex*ex + ey*ey)
	lat = math.Atan2(ez, r) * 180.0 / math.Pi
	alt = math.Sqrt(ex*ex+ey*ey+ez*ez) - 6371.0
	return
}

func fetchEphemeris(client *http.Client) ([]ephemerisPoint, error) {
	resp, err := client.Get(issOEMURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("NASA OEM request failed with HTTP %d", resp.StatusCode)
	}

	points := make([]ephemerisPoint, 0, 6000)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 7 || !strings.Contains(fields[0], "T") {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02T15:04:05.000", fields[0], time.UTC)
		if err != nil {
			continue
		}

		var point ephemerisPoint
		point.at = at.UTC()
		ok := true
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseFloat(fields[i+1], 64)
			if err != nil {
				ok = false
				break
			}
			point.pos[i] = v
		}
		for i := 0; i < 3 && ok; i++ {
			v, err := strconv.ParseFloat(fields[i+4], 64)
			if err != nil {
				ok = false
				break
			}
			point.vel[i] = v
		}
		if ok {
			points = append(points, point)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(points) < 2 {
		return nil, fmt.Errorf("NASA OEM response did not contain enough ephemeris points")
	}
	return points, nil
}

func interpolateEphemeris(points []ephemerisPoint, at time.Time) (pos [3]float64, vel [3]float64, ok bool) {
	if len(points) < 2 {
		return pos, vel, false
	}
	at = at.UTC()
	for i := 0; i < len(points)-1; i++ {
		a := points[i]
		b := points[i+1]
		if at.Before(a.at) || at.After(b.at) {
			continue
		}

		h := b.at.Sub(a.at).Seconds()
		if h <= 0 {
			return pos, vel, false
		}
		s := at.Sub(a.at).Seconds() / h
		s2 := s * s
		s3 := s2 * s

		for axis := 0; axis < 3; axis++ {
			y0 := a.pos[axis]
			y1 := b.pos[axis]
			m0 := a.vel[axis] * h
			m1 := b.vel[axis] * h

			pos[axis] = (2*s3-3*s2+1)*y0 +
				(s3-2*s2+s)*m0 +
				(-2*s3+3*s2)*y1 +
				(s3-s2)*m1
			vel[axis] = ((6*s2-6*s)*y0 +
				(3*s2-4*s+1)*m0 +
				(-6*s2+6*s)*y1 +
				(3*s2-2*s)*m1) / h
		}
		return pos, vel, true
	}
	return pos, vel, false
}

func velocityKMPerSec(vx, vy, vz float64) (float64, float64, float64) {
	speed := math.Sqrt(vx*vx + vy*vy + vz*vz)
	if speed > 20 {
		return vx / 1000.0, vy / 1000.0, vz / 1000.0
	}
	return vx, vy, vz
}

func propagateECI(pos [3]float64, vel [3]float64, dt float64) [3]float64 {
	if dt <= 0 {
		return pos
	}
	r := math.Sqrt(pos[0]*pos[0] + pos[1]*pos[1] + pos[2]*pos[2])
	if r == 0 {
		return pos
	}

	vx, vy, vz := velocityKMPerSec(vel[0], vel[1], vel[2])
	h := [3]float64{
		pos[1]*vz - pos[2]*vy,
		pos[2]*vx - pos[0]*vz,
		pos[0]*vy - pos[1]*vx,
	}
	hMag := math.Sqrt(h[0]*h[0] + h[1]*h[1] + h[2]*h[2])
	if hMag == 0 {
		return pos
	}
	axis := [3]float64{h[0] / hMag, h[1] / hMag, h[2] / hMag}

	const earthMu = 398600.4418 // km³/s²
	angle := math.Sqrt(earthMu/(r*r*r)) * dt
	c := math.Cos(angle)
	s := math.Sin(angle)
	dot := axis[0]*pos[0] + axis[1]*pos[1] + axis[2]*pos[2]

	return [3]float64{
		pos[0]*c + (axis[1]*pos[2]-axis[2]*pos[1])*s + axis[0]*dot*(1-c),
		pos[1]*c + (axis[2]*pos[0]-axis[0]*pos[2])*s + axis[1]*dot*(1-c),
		pos[2]*c + (axis[0]*pos[1]-axis[1]*pos[0])*s + axis[2]*dot*(1-c),
	}
}

func createLightstreamerSession(client *http.Client) (*lightstreamerSession, error) {
	resp, err := client.PostForm(serverURL+"/create_session.txt", url.Values{
		"LS_adapter_set": {adapterSet},
	})
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "SessionId:"); ok {
			sessionID := strings.TrimSpace(id)
			if sessionID == "" {
				resp.Body.Close()
				return nil, fmt.Errorf("empty session ID")
			}
			return &lightstreamerSession{id: sessionID, reader: reader, body: resp.Body}, nil
		}
	}
}

func subscribeLightstreamerTable(client *http.Client, sessionID string, tableID int, item telemetryItem, mode, schema string, snapshot bool) error {
	values := url.Values{
		"LS_session":      {sessionID},
		"LS_Table":        {strconv.Itoa(tableID)},
		"LS_op":           {"add"},
		"LS_data_adapter": {"DEFAULT"},
		"LS_mode":         {mode},
		"LS_schema":       {schema},
		"LS_id":           {item.itemID},
	}
	if snapshot {
		values.Set("LS_snapshot", "true")
		values.Set("LS_requested_snapshot", "yes")
	}

	resp, err := client.PostForm(serverURL+"/control.txt", values)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("control request failed with HTTP %d", resp.StatusCode)
	}
	if text := strings.TrimSpace(string(body)); strings.HasPrefix(text, "ERROR") || strings.HasPrefix(text, "REQERR") {
		return fmt.Errorf("control request rejected: %s", text)
	}
	return nil
}

func parseLightstreamerUpdate(line string) (tableID int, fields []string, eos bool, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil, false, false
	}

	if strings.HasSuffix(line, ",EOS") {
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return 0, nil, false, false
		}
		tableID, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, nil, false, false
		}
		return tableID, nil, true, true
	}

	comma := strings.IndexByte(line, ',')
	pipe := strings.IndexByte(line, '|')
	if comma < 0 || pipe < 0 || pipe < comma {
		return 0, nil, false, false
	}

	tableID, err := strconv.Atoi(line[:comma])
	if err != nil {
		return 0, nil, false, false
	}

	rawFields := strings.Split(line[pipe+1:], "|")
	fields = make([]string, len(rawFields))
	for i, field := range rawFields {
		fields[i] = decodeLightstreamerValue(field)
	}
	return tableID, fields, false, true
}

func decodeLightstreamerValue(value string) string {
	switch {
	case value == "$" || value == "#":
		return ""
	case strings.HasPrefix(value, "$$") || strings.HasPrefix(value, "##"):
		return value[1:]
	default:
		return value
	}
}

func startOfflineMonitor(pub *natsPublisher, timeout time.Duration) (func(), func()) {
	updates := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		online := true
		for {
			select {
			case <-done:
				return

			case <-updates:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(timeout)
				online = true

			case <-timer.C:
				if online {
					fmt.Printf("[ISS] No telemetry updates for %s; marking ISS offline\n", timeout)
					if err := pub.publish(map[string]map[string]any{
						"meta": {
							"online": false,
						},
					}); err != nil {
						fmt.Printf("[ISS] NATS publish error: %v\n", err)
					}
					online = false
				}
				timer.Reset(timeout)
			}
		}
	}()

	return func() {
			select {
			case updates <- struct{}{}:
			default:
			}
		}, func() {
			close(done)
		}
}

func startLightstreamerIdleWatch(session *lightstreamerSession, timeout time.Duration) (func(), func()) {
	values := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for {
			select {
			case <-done:
				return

			case <-values:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(timeout)

			case <-timer.C:
				fmt.Printf("[ISS] No Lightstreamer values for %s; reconnecting\n", timeout)
				_ = session.body.Close()
				return
			}
		}
	}()

	return func() {
			select {
			case values <- struct{}{}:
			default:
			}
		}, func() {
			close(done)
		}
}

func readMergeSnapshot(handleValue func(int, string)) error {
	return readSnapshot("MERGE", snapshotSchema, 1, handleValue)
}

func readSnapshot(mode, schema string, valueFieldIndex int, handleValue func(int, string)) error {
	client := &http.Client{Timeout: snapshotTimeout}
	session, err := createLightstreamerSession(client)
	if err != nil {
		return fmt.Errorf("create %s session: %w", mode, err)
	}
	defer session.body.Close()

	fmt.Printf("[ISS] %s snapshot session: %s\n", mode, session.id)
	for tableID, item := range items {
		if err := subscribeLightstreamerTable(client, session.id, tableID+1, item, mode, schema, true); err != nil {
			return fmt.Errorf("subscribe %s %s: %w", mode, item.itemID, err)
		}
	}

	received := map[int]bool{}
	eosTables := map[int]bool{}
	for len(eosTables) < len(items) && len(received) < len(items) {
		line, err := session.reader.ReadString('\n')
		if err != nil {
			if len(received) > 0 {
				fmt.Printf("[ISS] %s snapshot ended after %d/%d values: %v\n", mode, len(received), len(items), err)
				return nil
			}
			return fmt.Errorf("read %s snapshot: %w", mode, err)
		}

		tableID, fields, eos, ok := parseLightstreamerUpdate(line)
		if !ok {
			continue
		}
		if eos {
			eosTables[tableID] = true
			continue
		}
		idx := tableID - 1
		if idx < 0 || idx >= len(items) || len(fields) == 0 {
			continue
		}

		if isCommandDelete(fields) {
			continue
		}
		valueIndex := valueFieldIndex
		if valueIndex < 0 {
			valueIndex = len(fields) - 1
		}
		if valueIndex >= len(fields) {
			continue
		}
		value := fields[valueIndex]
		handleValue(idx, value)
		received[tableID] = true
	}

	fmt.Printf("[ISS] %s snapshot received %d/%d values\n", mode, len(received), len(items))
	return nil
}

func isCommandDelete(fields []string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, "DELETE") {
			return true
		}
	}
	return false
}

func Start() {
	pub, err := newNATSPublisher()
	if err != nil {
		fmt.Printf("[ISS] NATS unavailable, running without publishing: %v\n", err)
		pub = nil
	} else {
		fmt.Println("[ISS] NATS connected")
		defer pub.disconnect()
	}

	markUpdated := func() {}
	stopOfflineMonitor := func() {}
	if pub != nil {
		markUpdated, stopOfflineMonitor = startOfflineMonitor(pub, offlineTimeout)
		defer stopOfflineMonitor()
	}

	var stateMu sync.Mutex
	var ephemeris []ephemerisPoint
	var ephemerisLoadedAt time.Time

	// Tag names for MQTT payload, indexed to match items slice (idx 6 onward)
	mqttGroups := []string{"env", "env", "env", "env", "life", "life", "life", "life", "gnc", "gnc"}
	mqttTags := []string{"o2_pressure", "co2_pressure", "cabin_pressure", "cabin_temp", "urine_tank", "clean_water", "o2_gen_state", "urine_proc_state", "cmg1_status", "mode"}

	publishedDefs := map[string]bool{}
	tagValue := func(groupName, tagName string, value any, unit string, enumValues map[int]string, history bool) any {
		key := groupName + "." + tagName
		if (unit == "" && len(enumValues) == 0 && !history) || publishedDefs[key] {
			return value
		}
		publishedDefs[key] = true
		def := map[string]any{"value": value}
		if unit != "" {
			def["units"] = unit
		}
		if len(enumValues) > 0 {
			def["type"] = "enum"
			def["enumValues"] = enumValues
		}
		if history {
			def["history"] = true
		}
		return def
	}

	orbitClient := &http.Client{Timeout: snapshotTimeout}
	loadEphemeris := func() bool {
		points, err := fetchEphemeris(orbitClient)
		if err != nil {
			fmt.Printf("[ISS] NASA OEM ephemeris load failed: %v\n", err)
			return false
		}
		stateMu.Lock()
		ephemeris = points
		ephemerisLoadedAt = time.Now().UTC()
		stateMu.Unlock()
		fmt.Printf("[ISS] Loaded NASA OEM ephemeris: %d points from %s to %s\n",
			len(points), points[0].at.Format(time.RFC3339), points[len(points)-1].at.Format(time.RFC3339))
		return true
	}

	publishOrbit := func(now time.Time) {
		if pub == nil {
			return
		}
		now = now.UTC()

		stateMu.Lock()
		points := ephemeris
		loadedAt := ephemerisLoadedAt
		stateMu.Unlock()

		if len(points) < 2 || now.Sub(loadedAt) > orbitRefresh {
			if !loadEphemeris() && len(points) < 2 {
				return
			}
			stateMu.Lock()
			points = ephemeris
			stateMu.Unlock()
		}

		pos, vel, ok := interpolateEphemeris(points, now)
		if !ok {
			if !loadEphemeris() {
				return
			}
			stateMu.Lock()
			points = ephemeris
			stateMu.Unlock()
			pos, vel, ok = interpolateEphemeris(points, now)
			if !ok {
				fmt.Printf("[ISS] NASA OEM ephemeris has no point covering %s\n", now.Format(time.RFC3339))
				return
			}
		}

		lat, lon, alt := eciToLatLonAlt(pos[0], pos[1], pos[2], now)
		speed := math.Sqrt(vel[0]*vel[0] + vel[1]*vel[1] + vel[2]*vel[2])

		stateMu.Lock()
		groups := map[string]map[string]any{
			"meta": {
				"lat":          tagValue("meta", "lat", lat, "°", nil, false),
				"lon":          tagValue("meta", "lon", lon, "°", nil, false),
				"online":       true,
				"timeLastSeen": now.UnixMilli(),
			},
			"orbit": {
				"altitude": tagValue("orbit", "altitude", alt, "km", nil, false),
				"speed":    tagValue("orbit", "speed", speed, "km/s", nil, false),
				"pos_x":    tagValue("orbit", "pos_x", pos[0], "km", nil, false),
				"pos_y":    tagValue("orbit", "pos_y", pos[1], "km", nil, false),
				"pos_z":    tagValue("orbit", "pos_z", pos[2], "km", nil, false),
			},
		}
		stateMu.Unlock()

		if err := pub.publish(groups); err != nil {
			fmt.Printf("[ISS] NATS publish error: %v\n", err)
		} else {
			markUpdated()
		}
	}

	handleTelemetryValue := func(idx int, value string) {
		if idx < 0 || idx >= len(items) {
			return
		}
		item := items[idx]

		// Print to console
		// if item.unit != "" {
		// 	fmt.Printf("[ISS] %-24s %s %s\n", item.label, value, item.unit)
		// } else {
		// 	fmt.Printf("[ISS] %-24s %s\n", item.label, value)
		// }

		if idx < 6 {
			markUpdated()
			// The ISSLIVE state vector is not consistently live; use NASA's
			// timestamped OEM ephemeris for map position instead.
			return
		} else {
			markUpdated()

			// Non-state-vector item - publish its group immediately
			gi := idx - 6
			if gi >= 0 && gi < len(mqttTags) {
				groupName := mqttGroups[gi]
				tagName := mqttTags[gi]
				var parsed any
				integer, err := strconv.ParseInt(value, 10, 64)
				if err == nil {
					parsed = integer
				} else {
					flt, err := strconv.ParseFloat(value, 64)
					if err == nil {
						parsed = flt
					} else {
						parsed = value
					}
				}

				if pub != nil {
					stateMu.Lock()
					groups := map[string]map[string]any{
						groupName: {
							tagName: tagValue(groupName, tagName, parsed, item.unit, item.enumValues, item.history),
						},
						"meta": {
							"online":       true,
							"timeLastSeen": time.Now().UnixMilli(),
						},
					}
					stateMu.Unlock()
					if err := pub.publish(groups); err != nil {
						fmt.Printf("[ISS] MQTT publish error: %v\n", err)
					}
				}
			}
		}
	}

	if pub != nil {
		publishOrbit(time.Now().UTC())
		ticker := time.NewTicker(orbitPublish)
		defer ticker.Stop()
		go func() {
			for now := range ticker.C {
				publishOrbit(now)
			}
		}()
	}

	fmt.Println("[ISS] Connecting to Lightstreamer feed...")
	if err := readMergeSnapshot(handleTelemetryValue); err != nil {
		fmt.Printf("[ISS] MERGE snapshot failed: %v\n", err)
	}

	for {
		session, err := createLightstreamerSession(http.DefaultClient)
		if err != nil {
			fmt.Printf("[ISS] MERGE session failed: %v; retrying in 15s\n", err)
			time.Sleep(15 * time.Second)
			continue
		}
		fmt.Println("[ISS] Session:", session.id)

		subscribed := 0
		for tableID, item := range items {
			if err := subscribeLightstreamerTable(http.DefaultClient, session.id, tableID+1, item, "MERGE", "Value", false); err != nil {
				fmt.Printf("[ISS] MERGE subscribe error for %s: %v\n", item.itemID, err)
				continue
			}
			subscribed++
		}
		fmt.Printf("[ISS] Subscribed to %d/%d MERGE items. Listening...\n", subscribed, len(items))

		markLightstreamerValue, stopLightstreamerIdleWatch := startLightstreamerIdleWatch(session, lightstreamerIdleTimeout)
		for {
			line, err := session.reader.ReadString('\n')
			if err != nil {
				stopLightstreamerIdleWatch()
				fmt.Printf("[ISS] Connection closed: %v; reconnecting in 5s\n", err)
				_ = session.body.Close()
				time.Sleep(5 * time.Second)
				break
			}
			tableID, fields, eos, ok := parseLightstreamerUpdate(line)
			if !ok || eos || len(fields) == 0 {
				continue
			}
			markLightstreamerValue()
			handleTelemetryValue(tableID-1, fields[0])
		}
	}
}
