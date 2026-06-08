package main

import "fmt"

func buildProvisionPayload(deviceIndex int) payload {
	values := buildValuePayload(deviceIndex, 0)
	return payload{
		"meta": {
			"name": map[string]any{
				"value":       deviceName(deviceIndex),
				"description": "Benchmark device name",
				"persist":     true,
				"publish":     true,
			},
			"lat": map[string]any{
				"value":       values["meta"]["lat"],
				"units":       "deg",
				"description": "Benchmark latitude",
				"deadband":    0.0001,
				"publish":     true,
			},
			"lon": map[string]any{
				"value":       values["meta"]["lon"],
				"units":       "deg",
				"description": "Benchmark longitude",
				"deadband":    0.0001,
				"publish":     true,
			},
		},
		"analog": {
			"temperature": map[string]any{
				"value":       values["analog"]["temperature"],
				"units":       "C",
				"description": "Scaled temperature with limits, history, persist, stalecheck and publish",
				"deadband":    0.1,
				"history":     true,
				"persist":     true,
				"publish":     true,
				"stalecheck":  60,
				"scaling": map[string]any{
					"scale":  1.0,
					"offset": 0.0,
					"min":    -40.0,
					"max":    120.0,
				},
				"limits": map[string]any{
					"hi": 95.0,
					"lo": -10.0,
				},
			},
			"pressure": map[string]any{
				"value":       values["analog"]["pressure"],
				"units":       "kPa",
				"description": "Pressure with limits and history",
				"deadband":    0.2,
				"history":     true,
				"publish":     true,
				"limits": map[string]any{
					"hi": 250.0,
					"lo": 30.0,
				},
			},
			"flow": map[string]any{
				"value":       values["analog"]["flow"],
				"units":       "m3/h",
				"description": "Flow with scaling, history and publish",
				"deadband":    0.5,
				"history":     true,
				"publish":     true,
				"scaling": map[string]any{
					"scale":  1.05,
					"offset": 0.0,
					"min":    0.0,
					"max":    500.0,
				},
			},
			"vibration": map[string]any{
				"value":       values["analog"]["vibration"],
				"units":       "mm/s",
				"description": "Vibration with history and persist",
				"deadband":    0.05,
				"history":     true,
				"persist":     true,
				"publish":     true,
				"limits": map[string]any{
					"hi": 8.0,
					"lo": 0.0,
				},
			},
		},
		"status": {
			"online": map[string]any{
				"value":       values["status"]["online"],
				"description": "Online flag with persist and publish",
				"persist":     true,
				"publish":     true,
			},
			"state": map[string]any{
				"value":       values["status"]["state"],
				"description": "State string with persist and publish",
				"persist":     true,
				"publish":     true,
			},
			"mode": map[string]any{
				"value":       values["status"]["mode"],
				"type":        "enum",
				"description": "Enum mode with persist and publish",
				"persist":     true,
				"publish":     true,
				"enumValues": map[string]any{
					"0": "Off",
					"1": "Auto",
					"2": "Manual",
					"3": "Fault",
				},
			},
		},
		"counters": {
			"cycle_count": map[string]any{
				"value":       values["counters"]["cycle_count"],
				"description": "Monotonic counter with history",
				"deadband":    1.0,
				"history":     true,
				"publish":     true,
			},
			"quality": map[string]any{
				"value":       values["counters"]["quality"],
				"units":       "%",
				"description": "Quality percentage with limits and history",
				"deadband":    0.5,
				"history":     true,
				"publish":     true,
				"limits": map[string]any{
					"hi": 100.0,
					"lo": 70.0,
				},
			},
		},
	}
}

func buildValuePayload(deviceIndex int, seq int64) payload {
	f := float64(seq)
	d := float64(deviceIndex)
	state := "OK"
	if seq%97 == 0 {
		state = "WARN"
	} else if seq%211 == 0 {
		state = "BUSY"
	}

	return payload{
		"meta": {
			"lat": -33.9 + d*0.0001,
			"lon": 18.4 + d*0.0001,
		},
		"analog": {
			"temperature": 20.0 + float64((seq+int64(deviceIndex))%700)/20.0,
			"pressure":    100.0 + float64((seq*3+int64(deviceIndex))%800)/10.0,
			"flow":        50.0 + float64((seq*7+int64(deviceIndex))%3000)/10.0,
			"vibration":   1.0 + float64((seq*5+int64(deviceIndex))%120)/20.0,
		},
		"status": {
			"online": seq%113 != 0,
			"state":  state,
			"mode":   float64((seq + int64(deviceIndex)) % 4),
		},
		"counters": {
			"cycle_count": f,
			"quality":     75.0 + float64((seq+int64(deviceIndex))%250)/10.0,
		},
	}
}

func payloadSummary() string {
	return fmt.Sprintf("%d groups, %d tags", 4, 12)
}
