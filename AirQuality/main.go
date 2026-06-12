package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	defaultTenant = "default"
	defaultZone   = "LA_LongBeach"
	cityName      = "Los Angeles - Long Beach"

	templatesNode         = "Templates"
	airQualityDeviceType  = "AirQuality"
	standardTemplateNode  = "AirQualityStandard"
	backupTemplateNode    = "AirQualityBackup"
	standardTemplateName  = templatesNode + "." + standardTemplateNode
	backupTemplateName    = templatesNode + "." + backupTemplateNode
	defaultDeviceCount    = 25
	defaultUpdatePeriod   = 5 * time.Minute
	defaultStartupSpread  = 0
	defaultTemplateWait   = 3 * time.Second
	defaultPublishTimeout = 10 * time.Second
)

type payload map[string]any

type config struct {
	tenant         string
	zone           string
	devices        int
	updatePeriod   time.Duration
	startupSpread  time.Duration
	templateWait   time.Duration
	provisionOnly  bool
	publishTimeout time.Duration
	mqttURL        string
	mqttUsername   string
	mqttPassword   string
	mqttClientID   string
	mqttQoS        int
}

type deviceVariation string

const (
	variationStandard deviceVariation = "standard"
	variationBackup   deviceVariation = "battery-backup"
)

type device struct {
	Name           string
	Type           string
	Variation      deviceVariation
	Intersection   string
	Lat            float64
	Lon            float64
	Seed           uint64
	Seq            int64
	NextDue        time.Time
	BatteryPercent float64
	MainsFail      bool
}

type intersectionAnchor struct {
	Name string
	Lat  float64
	Lon  float64
}

// These anchors are major Los Angeles and Long Beach road junctions. The
// generated fleet creates at most one device at each intersection.
var laLongBeachIntersections = []intersectionAnchor{
	{Name: "US-101 and Alameda Street", Lat: 34.0566, Lon: -118.2351},
	{Name: "I-10 and I-110 Interchange", Lat: 34.0398, Lon: -118.2675},
	{Name: "Wilshire Boulevard and Vermont Avenue", Lat: 34.0617, Lon: -118.2917},
	{Name: "Hollywood Boulevard and Highland Avenue", Lat: 34.1016, Lon: -118.3389},
	{Name: "Sunset Boulevard and Vine Street", Lat: 34.0987, Lon: -118.3267},
	{Name: "La Brea Avenue and Olympic Boulevard", Lat: 34.0578, Lon: -118.3440},
	{Name: "Crenshaw Boulevard and Slauson Avenue", Lat: 33.9894, Lon: -118.3301},
	{Name: "Florence Avenue and Vermont Avenue", Lat: 33.9745, Lon: -118.2917},
	{Name: "Imperial Highway and Sepulveda Boulevard", Lat: 33.9305, Lon: -118.3968},
	{Name: "Century Boulevard and Aviation Boulevard", Lat: 33.9456, Lon: -118.3784},
	{Name: "Santa Monica Boulevard and Sepulveda Boulevard", Lat: 34.0445, Lon: -118.4454},
	{Name: "Ventura Boulevard and Sepulveda Boulevard", Lat: 34.1518, Lon: -118.4488},
	{Name: "Colorado Boulevard and Fair Oaks Avenue", Lat: 34.1457, Lon: -118.1505},
	{Name: "Atlantic Boulevard and Valley Boulevard", Lat: 34.0792, Lon: -118.1345},
	{Name: "Whittier Boulevard and Atlantic Boulevard", Lat: 34.0253, Lon: -118.1562},
	{Name: "I-5 and Washington Boulevard", Lat: 33.9966, Lon: -118.1668},
	{Name: "Firestone Boulevard and Lakewood Boulevard", Lat: 33.9403, Lon: -118.1337},
	{Name: "Rosecrans Avenue and Lakewood Boulevard", Lat: 33.9038, Lon: -118.1426},
	{Name: "Artesia Boulevard and Atlantic Avenue", Lat: 33.8736, Lon: -118.1847},
	{Name: "Pacific Coast Highway and Anaheim Street", Lat: 33.7828, Lon: -118.1931},
	{Name: "Long Beach Boulevard and Willow Street", Lat: 33.8044, Lon: -118.1899},
	{Name: "Long Beach Boulevard and Anaheim Street", Lat: 33.7821, Lon: -118.1898},
	{Name: "Ocean Boulevard and Pine Avenue", Lat: 33.7676, Lon: -118.1926},
	{Name: "Terminal Island Freeway and Anaheim Street", Lat: 33.7802, Lon: -118.2249},
	{Name: "Harbor Scenic Drive and Ocean Boulevard", Lat: 33.7540, Lon: -118.2077},
}

func main() {
	cfg := parseFlags()
	if err := cfg.validate(); err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	publisher := newMQTTPublisher(cfg)
	if err := publisher.connect(); err != nil {
		log.Fatalf("MQTT connect: %v", err)
	}
	defer publisher.disconnect()

	devices, err := buildDevices(cfg.devices)
	if err != nil {
		log.Fatalf("build devices: %v", err)
	}

	if err := ensureXACTSetup(ctx, cfg, publisher); err != nil {
		log.Fatalf("setup XACT templates: %v", err)
	}

	standardCount, backupCount := countDeviceVariations(devices)
	log.Printf("AirQuality: provisioning %d %s air-quality devices under %s (%d %s, %d %s)",
		len(devices), cityName, airQualityDeviceType, standardCount, variationStandard, backupCount, variationBackup)
	if err := publishInitialFleet(ctx, cfg, publisher, devices); err != nil {
		log.Fatalf("initial fleet publish: %v", err)
	}
	if cfg.provisionOnly {
		log.Printf("AirQuality: provision-only complete")
		return
	}

	log.Printf("AirQuality: simulation running, update period %s spread across %d devices", cfg.updatePeriod, len(devices))
	runSimulation(ctx, cfg, publisher, devices)
}

func parseFlags() config {
	cfg := config{
		tenant:         envDefault("XACT_TENANT", defaultTenant),
		zone:           envDefault("AIRQUALITY_ZONE", defaultZone),
		devices:        envIntDefault("AIRQUALITY_DEVICES", defaultDeviceCount),
		updatePeriod:   envDurationDefault("AIRQUALITY_UPDATE_PERIOD", defaultUpdatePeriod),
		startupSpread:  envDurationDefault("AIRQUALITY_STARTUP_SPREAD", defaultStartupSpread),
		templateWait:   envDurationDefault("AIRQUALITY_TEMPLATE_WAIT", defaultTemplateWait),
		publishTimeout: envDurationDefault("AIRQUALITY_PUBLISH_TIMEOUT", defaultPublishTimeout),
		mqttURL:        envDefault("MQTT_BROKER", "tcp://127.0.0.1:1883"),
		mqttUsername:   envDefault("MQTT_USERNAME", "airquality"),
		mqttPassword:   envDefault("MQTT_PASSWORD", "xact"),
		mqttClientID:   envDefault("MQTT_CLIENT_ID", fmt.Sprintf("xact-airquality-%d", time.Now().UnixNano())),
		mqttQoS:        envIntDefault("MQTT_QOS", 1),
	}

	flag.StringVar(&cfg.tenant, "tenant", cfg.tenant, "XACT organisation/tenant")
	flag.StringVar(&cfg.zone, "zone", cfg.zone, "XACT zone used for the simulated city")
	flag.IntVar(&cfg.devices, "devices", cfg.devices, "number of simulated devices; capped at one per intersection")
	flag.DurationVar(&cfg.updatePeriod, "period", cfg.updatePeriod, "target update period for each device")
	flag.DurationVar(&cfg.startupSpread, "startup-spread", cfg.startupSpread, "optional duration over which initial device messages are spread; 0 publishes as fast as MQTT acknowledges")
	flag.DurationVar(&cfg.templateWait, "template-wait", cfg.templateWait, "time to let MQTT-provisioned templates settle before devices are created")
	flag.DurationVar(&cfg.publishTimeout, "publish-timeout", cfg.publishTimeout, "MQTT publish wait timeout")
	flag.BoolVar(&cfg.provisionOnly, "provision-only", cfg.provisionOnly, "create templates/devices once and exit")

	flag.StringVar(&cfg.mqttURL, "mqtt-url", cfg.mqttURL, "MQTT broker URL")
	flag.StringVar(&cfg.mqttUsername, "mqtt-username", cfg.mqttUsername, "MQTT username")
	flag.StringVar(&cfg.mqttPassword, "mqtt-password", cfg.mqttPassword, "MQTT password")
	flag.StringVar(&cfg.mqttClientID, "mqtt-client-id", cfg.mqttClientID, "MQTT client ID")
	flag.IntVar(&cfg.mqttQoS, "mqtt-qos", cfg.mqttQoS, "MQTT publish QoS")

	flag.Parse()

	return cfg
}

func (cfg config) validate() error {
	if cfg.devices <= 0 {
		return errors.New("devices must be > 0")
	}
	if cfg.devices > len(laLongBeachIntersections) {
		return fmt.Errorf("devices must be <= %d so each intersection has at most one device", len(laLongBeachIntersections))
	}
	if cfg.updatePeriod < time.Minute {
		return errors.New("period must be at least 1 minute for this low-throughput simulation")
	}
	if cfg.startupSpread < 0 {
		return errors.New("startup-spread cannot be negative")
	}
	if cfg.mqttQoS < 0 || cfg.mqttQoS > 2 {
		return errors.New("mqtt-qos must be 0, 1, or 2")
	}
	for _, token := range []string{cfg.tenant, cfg.zone, airQualityDeviceType, standardTemplateNode, backupTemplateNode} {
		if !validTopicToken(token) {
			return fmt.Errorf("invalid topic/path token %q", token)
		}
	}
	if cfg.mqttURL == "" {
		return errors.New("mqtt-url is required")
	}
	return nil
}

func validTopicToken(token string) bool {
	return token != "" && !strings.ContainsAny(token, "/. >*#")
}

func ensureXACTSetup(ctx context.Context, cfg config, publisher *mqttPublisher) error {
	if err := publisher.publish(ctx, buildTopic(cfg.tenant, "", templatesNode, standardTemplateNode), buildTemplatePayload(false)); err != nil {
		return fmt.Errorf("publish %s template: %w", standardTemplateNode, err)
	}
	if err := publisher.publish(ctx, buildTopic(cfg.tenant, "", templatesNode, backupTemplateNode), buildTemplatePayload(true)); err != nil {
		return fmt.Errorf("publish %s template: %w", backupTemplateNode, err)
	}

	if cfg.templateWait > 0 {
		log.Printf("AirQuality: waiting %s for template ingest", cfg.templateWait)
		if err := sleepContext(ctx, cfg.templateWait); err != nil {
			return err
		}
	}

	log.Printf("AirQuality: templates ready (%s, %s)", standardTemplateName, backupTemplateName)
	return nil
}

func buildTemplatePayload(hasBattery bool) payload {
	subtype := "AQ-STD"
	templateNode := standardTemplateNode
	description := "Standard street-corner air-quality monitor template"
	if hasBattery {
		subtype = "AQ-BBU"
		templateNode = backupTemplateNode
		description = "Battery-backup street-corner air-quality monitor template"
	}

	p := payload{
		"meta": map[string]any{
			"name":          textTag(templateNode, "Template display name", true),
			"deviceFamily":  textTag("AirQuality", "Device family", true),
			"deviceSubtype": textTag(subtype, "Device variation", true),
			"description":   textTag(description, "Device description", true),
			"intersection":  textTag(cityName+" street corner", "Nearest street intersection", true),
			"firmware":      textTag("AQ-4.2.0", "Firmware version", true),
			"lat":           numericTag(33.94, "Latitude", "deg", 0.0001, nil, true),
			"lon":           numericTag(-118.24, "Longitude", "deg", 0.0001, nil, true),
			"timeLastSeen":  numericTag(float64(time.Now().UnixMilli()), "Last telemetry timestamp", "ms", 1, nil, true),
			"online":        boolTag(true, "Device online", true),
		},
		"particulate": map[string]any{
			"pm1":  numericTag(42.0, "PM1.0 particulate concentration", "ug/m3", 1.0, &limits{Lo: 0, Hi: 100}, true),
			"pm25": numericTag(88.0, "PM2.5 particulate concentration", "ug/m3", 1.0, &limits{Lo: 0, Hi: 150}, true),
			"pm10": numericTag(162.0, "PM10 particulate concentration", "ug/m3", 2.0, &limits{Lo: 0, Hi: 300}, true),
		},
		"gas": map[string]any{
			"co2": numericTag(680.0, "Carbon dioxide concentration", "ppm", 5.0, &limits{Lo: 350, Hi: 1200}, true),
			"no2": numericTag(64.0, "Nitrogen dioxide concentration", "ppb", 1.0, &limits{Lo: 0, Hi: 200}, true),
			"so2": numericTag(12.0, "Sulphur dioxide concentration", "ppb", 0.5, &limits{Lo: 0, Hi: 75}, true),
			"o3":  numericTag(32.0, "Ozone concentration", "ppb", 1.0, &limits{Lo: 0, Hi: 100}, true),
			"co":  numericTag(1.2, "Carbon monoxide concentration", "ppm", 0.1, &limits{Lo: 0, Hi: 9}, true),
		},
		"env": map[string]any{
			"temperature": numericTag(23.0, "Ambient temperature", "C", 0.2, &limits{Lo: 4, Hi: 42}, true),
			"humidity":    numericTag(58.0, "Relative humidity", "%", 1.0, &limits{Lo: 15, Hi: 95}, true),
			"pressure":    numericTag(1005.0, "Barometric pressure", "hPa", 0.5, &limits{Lo: 950, Hi: 1035}, true),
			"noise":       numericTag(70.0, "Roadside noise level", "dBA", 1.0, &limits{Lo: 35, Hi: 85}, true),
		},
		"air": map[string]any{
			"aqi":      numericTag(172.0, "Composite air quality index", "AQI", 1.0, &limits{Lo: 0, Hi: 300}, true),
			"category": textTag("Unhealthy", "AQI category", true),
		},
		"status": map[string]any{
			"sampleFlow":         numericTag(1.0, "Sensor sample flow", "L/min", 0.02, &limits{Lo: 0.85, Hi: 1.15}, true),
			"sensorFault":        boolTag(false, "Sensor fault present", true),
			"calibrationDueDays": numericTag(21, "Days until calibration is due", "d", 1, &limits{Lo: 3, Hi: 365}, true),
			"uptimeHours":        numericTag(100, "Device uptime", "h", 1, nil, true),
		},
	}

	if hasBattery {
		p["power"] = map[string]any{
			"batteryVoltage": numericTag(12.8, "Backup battery voltage", "V", 0.05, &limits{Lo: 11.2, Hi: 14.8}, true),
			"batteryPercent": numericTag(86, "Backup battery state of charge", "%", 1.0, &limits{Lo: 20, Hi: 100}, true),
			"mainsFail":      boolTag(false, "Mains power failure", true),
			"charging":       boolTag(true, "Battery charging", true),
			"backupMinutes":  numericTag(360, "Estimated backup runtime", "min", 5, &limits{Lo: 30, Hi: 600}, true),
		}
	}

	return p
}

type limits struct {
	Lo float64
	Hi float64
}

func numericTag(value float64, description, units string, deadband float64, lim *limits, history bool) map[string]any {
	tag := map[string]any{
		"value":       value,
		"description": description,
		"units":       units,
		"publish":     true,
	}
	if deadband > 0 {
		tag["deadband"] = deadband
	}
	if history {
		tag["history"] = true
	}
	if lim != nil {
		tag["limits"] = map[string]any{
			"lo": lim.Lo,
			"hi": lim.Hi,
			"hiEvent": map[string]any{
				"enabled":        true,
				"severity":       "WARN",
				"message":        description + " high limit",
				"notificationId": "none",
			},
			"lowEvent": map[string]any{
				"enabled":        lim.Lo > 0,
				"severity":       "WARN",
				"message":        description + " low limit",
				"notificationId": "none",
			},
			"rtnEvent": map[string]any{
				"enabled":        true,
				"severity":       "INFO",
				"message":        description + " returned to normal",
				"notificationId": "none",
			},
		}
	}
	return tag
}

func textTag(value, description string, _ bool) map[string]any {
	return map[string]any{
		"value":       value,
		"description": description,
		"publish":     true,
	}
}

func boolTag(value bool, description string, _ bool) map[string]any {
	return map[string]any{
		"value":       value,
		"description": description,
		"publish":     true,
	}
}

func buildDevices(count int) ([]*device, error) {
	if count <= 0 {
		return nil, errors.New("device count must be positive")
	}
	if count > len(laLongBeachIntersections) {
		return nil, fmt.Errorf("device count must be <= %d", len(laLongBeachIntersections))
	}

	devices := make([]*device, 0, count)
	standardIndex := 0
	backupIndex := 0
	for i := 0; i < count; i++ {
		variation := variationStandard
		prefix := "AQ-S"
		variationIndex := standardIndex + 1
		if i%2 == 1 {
			variation = variationBackup
			prefix = "AQ-B"
			variationIndex = backupIndex + 1
		}

		anchor := laLongBeachIntersections[i]
		seed := hash64(fmt.Sprintf("%s-%d-%s", prefix, variationIndex, anchor.Name))
		devices = append(devices, &device{
			Name:           fmt.Sprintf("%s-%04d", prefix, variationIndex),
			Type:           airQualityDeviceType,
			Variation:      variation,
			Intersection:   anchor.Name,
			Lat:            anchor.Lat,
			Lon:            anchor.Lon,
			Seed:           seed,
			BatteryPercent: 70 + unitNoise(seed, 0, 91)*28,
		})
		if variation == variationBackup {
			backupIndex++
		} else {
			standardIndex++
		}
	}
	return devices, nil
}

func countDeviceVariations(devices []*device) (standard, backup int) {
	for _, d := range devices {
		switch d.Variation {
		case variationStandard:
			standard++
		case variationBackup:
			backup++
		}
	}
	return standard, backup
}

func publishInitialFleet(ctx context.Context, cfg config, publisher *mqttPublisher, devices []*device) error {
	delay := time.Duration(0)
	if cfg.startupSpread > 0 && len(devices) > 1 {
		delay = cfg.startupSpread / time.Duration(len(devices)-1)
	}

	for i, d := range devices {
		if i > 0 && delay > 0 {
			if err := sleepContext(ctx, delay); err != nil {
				return err
			}
		}
		now := time.Now()
		if err := publishDevice(ctx, cfg, publisher, d, now); err != nil {
			return fmt.Errorf("%s: %w", d.Name, err)
		}
		scheduleNext(d, now, cfg.updatePeriod)
		if (i+1)%50 == 0 || i+1 == len(devices) {
			log.Printf("AirQuality: initial device messages %d/%d", i+1, len(devices))
		}
	}
	return nil
}

func runSimulation(ctx context.Context, cfg config, publisher *mqttPublisher, devices []*device) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("AirQuality: stopping")
			return
		case now := <-ticker.C:
			published := 0
			for _, d := range devices {
				if now.Before(d.NextDue) {
					continue
				}
				if err := publishDevice(ctx, cfg, publisher, d, now); err != nil {
					log.Printf("AirQuality: publish %s: %v", d.Name, err)
				} else {
					published++
				}
				scheduleNext(d, now, cfg.updatePeriod)
			}
			if published > 0 {
				log.Printf("AirQuality: published %d scheduled updates", published)
			}
		}
	}
}

func publishDevice(ctx context.Context, cfg config, publisher *mqttPublisher, d *device, now time.Time) error {
	d.Seq++
	return publisher.publish(ctx, buildTopic(cfg.tenant, cfg.zone, d.Type, d.Name), buildTelemetryPayload(d, now))
}

func scheduleNext(d *device, now time.Time, base time.Duration) {
	jitterSpan := int64(base / 8)
	if jitterSpan < int64(10*time.Second) {
		jitterSpan = int64(10 * time.Second)
	}
	jitter := time.Duration(int64(unitNoise(d.Seed, d.Seq, 701)*float64(jitterSpan*2)) - jitterSpan)
	d.NextDue = now.Add(base + jitter)
}

func buildTelemetryPayload(d *device, now time.Time) payload {
	hour := float64(now.Hour()) + float64(now.Minute())/60
	rush := rushHourFactor(hour)
	heat := math.Sin((hour - 7) / 24 * 2 * math.Pi)
	ozoneSun := math.Max(0, math.Sin((hour-9)/12*math.Pi))
	local := unitNoise(d.Seed, 0, 11)
	seq := d.Seq

	pm25 := clamp(60+55*rush+25*local+18*unitNoise(d.Seed, seq, 1), 12, 280)
	if int64(d.Seed%173) == seq%173 {
		pm25 += 60
	}
	pm10 := clamp(pm25*1.75+25*unitNoise(d.Seed, seq, 2), 25, 480)
	pm1 := clamp(pm25*0.52+8*unitNoise(d.Seed, seq, 3), 4, 170)

	no2 := clamp(30+70*rush+25*unitNoise(d.Seed, seq, 4), 5, 260)
	so2 := clamp(5+18*unitNoise(d.Seed, seq, 5)+8*rush, 1, 95)
	o3 := clamp(15+75*ozoneSun+10*unitNoise(d.Seed, seq, 6), 4, 140)
	co := clamp(0.4+2.2*rush+0.8*unitNoise(d.Seed, seq, 7), 0.1, 12)
	co2 := clamp(430+280*rush+180*unitNoise(d.Seed, seq, 8), 390, 1700)

	temp := clamp(21+6*heat+2.0*unitNoise(d.Seed, seq, 9), 6, 42)
	humidity := clamp(62-18*heat+10*unitNoise(d.Seed, seq, 10), 18, 96)
	pressure := clamp(1006+4*math.Sin(float64(now.YearDay())/365*2*math.Pi)+2*unitNoise(d.Seed, seq, 12), 960, 1038)
	noise := clamp(55+22*rush+8*unitNoise(d.Seed, seq, 13), 38, 95)
	aqi := clamp(pm25*1.8+pm10*0.22+no2*0.25+o3*0.2+co*4, 20, 500)

	p := payload{
		"ts":           now.UnixMilli(),
		"templateName": templateNameForDevice(d),
		"description":  fmt.Sprintf("%s %s monitor at %s", d.Variation, d.Type, d.Intersection),
		"meta": map[string]any{
			"name":          d.Name,
			"deviceFamily":  "AirQuality",
			"deviceSubtype": string(d.Variation),
			"intersection":  d.Intersection,
			"firmware":      firmwareVersion(d),
			"lat":           d.Lat,
			"lon":           d.Lon,
			"timeLastSeen":  now.UnixMilli(),
			"online":        true,
		},
		"particulate": map[string]any{
			"pm1":  round(pm1, 1),
			"pm25": round(pm25, 1),
			"pm10": round(pm10, 1),
		},
		"gas": map[string]any{
			"co2": round(co2, 0),
			"no2": round(no2, 1),
			"so2": round(so2, 1),
			"o3":  round(o3, 1),
			"co":  round(co, 2),
		},
		"env": map[string]any{
			"temperature": round(temp, 1),
			"humidity":    round(humidity, 1),
			"pressure":    round(pressure, 1),
			"noise":       round(noise, 1),
		},
		"air": map[string]any{
			"aqi":      round(aqi, 0),
			"category": aqiCategory(aqi),
		},
		"status": map[string]any{
			"sampleFlow":         round(0.98+0.05*unitNoise(d.Seed, seq, 15), 2),
			"sensorFault":        unitNoise(d.Seed, seq, 16) > 0.995,
			"calibrationDueDays": int(3 + math.Mod(float64(d.Seed>>8)+float64(seq/288), 45)),
			"uptimeHours":        int(24 + seq/12 + int64(d.Seed%4000)),
		},
	}

	if d.Variation == variationBackup {
		updateBattery(d)
		charging := !d.MainsFail && d.BatteryPercent < 99.5
		voltage := 11.15 + (d.BatteryPercent/100)*2.05
		if charging {
			voltage += 0.25
		}
		p["power"] = map[string]any{
			"batteryVoltage": round(voltage, 2),
			"batteryPercent": round(d.BatteryPercent, 1),
			"mainsFail":      d.MainsFail,
			"charging":       charging,
			"backupMinutes":  int(clamp(d.BatteryPercent*4.6, 0, 600)),
		}
	}

	return p
}

func updateBattery(d *device) {
	trip := unitNoise(d.Seed, d.Seq, 201)
	restore := unitNoise(d.Seed, d.Seq, 202)
	if !d.MainsFail && trip > 0.996 {
		d.MainsFail = true
	}
	if d.MainsFail && restore > 0.65 {
		d.MainsFail = false
	}

	if d.MainsFail {
		d.BatteryPercent = clamp(d.BatteryPercent-(0.7+unitNoise(d.Seed, d.Seq, 203)*0.8), 0, 100)
	} else {
		d.BatteryPercent = clamp(d.BatteryPercent+0.3+unitNoise(d.Seed, d.Seq, 204)*0.4, 0, 100)
	}
}

func rushHourFactor(hour float64) float64 {
	morning := math.Exp(-math.Pow(hour-9, 2) / 5.5)
	evening := math.Exp(-math.Pow(hour-18.5, 2) / 7)
	nightInversion := 0.4 * math.Exp(-math.Pow(hour-1.5, 2)/10)
	return clamp(morning+evening+nightInversion, 0, 1.4)
}

func aqiCategory(aqi float64) string {
	switch {
	case aqi <= 50:
		return "Good"
	case aqi <= 100:
		return "Moderate"
	case aqi <= 150:
		return "UnhealthySensitive"
	case aqi <= 200:
		return "Unhealthy"
	case aqi <= 300:
		return "VeryUnhealthy"
	default:
		return "Hazardous"
	}
}

func firmwareVersion(d *device) string {
	if d.Variation == variationBackup {
		return fmt.Sprintf("AQ-BBU-4.%d.%d", d.Seed%5, d.Seed%17)
	}
	return fmt.Sprintf("AQ-STD-4.%d.%d", d.Seed%5, d.Seed%17)
}

func templateNameForDevice(d *device) string {
	if d.Variation == variationBackup {
		return backupTemplateName
	}
	return standardTemplateName
}

func buildTopic(tenant, zone, deviceType, deviceName string) string {
	if zone == "" {
		return fmt.Sprintf("xact/data/%s/%s/%s", tenant, deviceType, deviceName)
	}
	return fmt.Sprintf("xact/data/%s/zone/%s/%s/%s", tenant, zone, deviceType, deviceName)
}

type mqttPublisher struct {
	cfg    config
	client mqtt.Client
}

func newMQTTPublisher(cfg config) *mqttPublisher {
	return &mqttPublisher{cfg: cfg}
}

func (p *mqttPublisher) connect() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(p.cfg.mqttURL)
	opts.SetClientID(p.cfg.mqttClientID)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetOrderMatters(false)
	if p.cfg.mqttUsername != "" {
		opts.SetUsername(p.cfg.mqttUsername)
	}
	if p.cfg.mqttPassword != "" {
		opts.SetPassword(p.cfg.mqttPassword)
	}
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Printf("AirQuality: MQTT connection lost: %v", err)
	})

	p.client = mqtt.NewClient(opts)
	token := p.client.Connect()
	if !token.WaitTimeout(p.cfg.publishTimeout) {
		return errors.New("MQTT connect timeout")
	}
	if token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (p *mqttPublisher) disconnect() {
	if p.client != nil && p.client.IsConnected() {
		p.client.Disconnect(250)
	}
}

func (p *mqttPublisher) publish(ctx context.Context, topic string, body payload) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	token := p.client.Publish(topic, byte(p.cfg.mqttQoS), false, data)
	done := make(chan struct{})
	go func() {
		token.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.cfg.publishTimeout):
		return fmt.Errorf("MQTT publish timeout for %s", topic)
	case <-done:
		if token.Error() != nil {
			return token.Error()
		}
		return nil
	}
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envIntDefault(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func envDurationDefault(name string, fallback time.Duration) time.Duration {
	if value := os.Getenv(name); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func hash64(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func unitNoise(seed uint64, seq int64, salt uint64) float64 {
	x := seed + uint64(seq)*0x9e3779b97f4a7c15 + salt*0xbf58476d1ce4e5b9
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return float64(x>>11) / (1 << 53)
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func round(v float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(v*scale) / scale
}
