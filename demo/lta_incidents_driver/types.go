package lta_incidents_driver

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

const (
	DefaultLTABaseURL       = "https://datamall2.mytransport.sg/ltaodataservice"
	DefaultXACTBaseURL      = "https://127.0.0.1:8443/xact"
	DefaultTenant           = "default"
	DefaultZone             = "Singapore"
	IncidentsDeviceType     = "Incidents"
	defaultCoordinateFormat = "%.8f"
)

type TrafficIncidentsResponse struct {
	Value []TrafficIncident `json:"value"`
}

type TrafficIncident struct {
	Type      string  `json:"Type"`
	Latitude  float64 `json:"Latitude"`
	Longitude float64 `json:"Longitude"`
	Message   string  `json:"Message"`
}

func (i TrafficIncident) CoordinateKey() string {
	return coordinateKey(i.Latitude, i.Longitude)
}

func (i TrafficIncident) DeviceName() string {
	return deviceNameForCoordinates(i.Latitude, i.Longitude)
}

func coordinateKey(lat, lon float64) string {
	return fmt.Sprintf(defaultCoordinateFormat+","+defaultCoordinateFormat, lat, lon)
}

func deviceNameForCoordinates(lat, lon float64) string {
	key := coordinateKey(lat, lon)
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return "INC_" + strings.ToUpper(strconv.FormatUint(uint64(h.Sum32()), 36))
}
