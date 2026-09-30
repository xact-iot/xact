package api

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxBusGTFSZipBytes = 25 << 20
const maxBusGTFSRequestBytes = 36 << 20 // Base64-encoded ZIP plus JSON envelope.
const maxBusGTFSCSVBytes = 128 << 20
const maxBusGTFSExpandedBytes = 256 << 20

type busFeed struct {
	Stops     map[string]busStop
	Routes    map[string]busRoute
	Schedule  map[string]busSchedule
	Calendars map[string]busCalendar
	Trips     int
}
type busTrip struct{ RouteID, ServiceID, Headsign string }

func readBusCSV(archive *zip.Reader, name string, maxRows int) ([]map[string]string, error) {
	var file *zip.File
	for _, entry := range archive.File {
		if strings.EqualFold(path.Base(entry.Name), name) {
			file = entry
			break
		}
	}
	if file == nil {
		return nil, fmt.Errorf("GTFS archive is missing %s", name)
	}
	if file.UncompressedSize64 > maxBusGTFSCSVBytes {
		return nil, fmt.Errorf("%s exceeds %d MiB", name, maxBusGTFSCSVBytes>>20)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	csvReader := csv.NewReader(io.LimitReader(reader, maxBusGTFSCSVBytes+1))
	csvReader.FieldsPerRecord = -1
	headers, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for i := range headers {
		headers[i] = strings.TrimSpace(strings.TrimPrefix(headers[i], "\ufeff"))
	}
	rows := make([]map[string]string, 0)
	for {
		values, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if len(rows) >= maxRows {
			return nil, fmt.Errorf("%s has too many rows", name)
		}
		row := make(map[string]string, len(headers))
		for i, key := range headers {
			if i < len(values) {
				row[key] = strings.TrimSpace(values[i])
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func busGTFSDate(raw string) string {
	if len(raw) != 8 {
		return ""
	}
	value := raw[:4] + "-" + raw[4:6] + "-" + raw[6:]
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}
func optionalBusCSV(archive *zip.Reader, name string, maxRows int) ([]map[string]string, error) {
	for _, file := range archive.File {
		if strings.EqualFold(path.Base(file.Name), name) {
			return readBusCSV(archive, name, maxRows)
		}
	}
	return nil, nil
}
func validateBusGTFSArchiveSizes(files []*zip.File) error {
	// shapes.txt and other unused GTFS files are never decompressed here.
	// Bound only the tables that this importer actually reads.
	usedTables := map[string]bool{
		"stops.txt": true, "routes.txt": true, "trips.txt": true,
		"stop_times.txt": true, "calendar.txt": true, "calendar_dates.txt": true,
	}
	var expanded uint64
	for _, file := range files {
		name := strings.ToLower(path.Base(file.Name))
		if !usedTables[name] {
			continue
		}
		if file.UncompressedSize64 > maxBusGTFSCSVBytes {
			return fmt.Errorf("%s exceeds %d MiB", name, maxBusGTFSCSVBytes>>20)
		}
		if file.UncompressedSize64 > maxBusGTFSExpandedBytes-expanded {
			return fmt.Errorf("GTFS tables expand beyond %d MiB", maxBusGTFSExpandedBytes>>20)
		}
		expanded += file.UncompressedSize64
	}
	return nil
}

func parseBusGTFS(encoded string) (busFeed, error) {
	feed := busFeed{Stops: map[string]busStop{}, Routes: map[string]busRoute{}, Schedule: map[string]busSchedule{}, Calendars: map[string]busCalendar{}}
	if len(encoded) > base64.StdEncoding.EncodedLen(maxBusGTFSZipBytes) {
		return feed, errors.New("ZIP file is too large")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) > maxBusGTFSZipBytes {
		return feed, errors.New("Invalid or oversized ZIP file")
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return feed, errors.New("Invalid GTFS ZIP file")
	}
	if len(archive.File) > 64 {
		return feed, errors.New("GTFS archive has too many files")
	}
	if err := validateBusGTFSArchiveSizes(archive.File); err != nil {
		return feed, err
	}
	stops, err := readBusCSV(archive, "stops.txt", 10000)
	if err != nil {
		return feed, err
	}
	routes, err := readBusCSV(archive, "routes.txt", 2000)
	if err != nil {
		return feed, err
	}
	trips, err := readBusCSV(archive, "trips.txt", 25000)
	if err != nil {
		return feed, err
	}
	times, err := readBusCSV(archive, "stop_times.txt", 100000)
	if err != nil {
		return feed, err
	}

	calendarRows, err := optionalBusCSV(archive, "calendar.txt", 10000)
	if err != nil {
		return feed, err
	}
	for _, row := range calendarRows {
		id := row["service_id"]
		start := busGTFSDate(row["start_date"])
		end := busGTFSDate(row["end_date"])
		if id == "" || start == "" || end == "" {
			continue
		}
		days := [7]string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
		calendar := busCalendar{Start: start, End: end, Exceptions: map[string]int{}}
		for i, name := range days {
			calendar.Weekdays[i] = row[name] == "1"
		}
		feed.Calendars[id] = calendar
	}
	exceptionRows, err := optionalBusCSV(archive, "calendar_dates.txt", 100000)
	if err != nil {
		return feed, err
	}
	for _, row := range exceptionRows {
		id := row["service_id"]
		date := busGTFSDate(row["date"])
		kind, _ := strconv.Atoi(row["exception_type"])
		if id == "" || date == "" || (kind != 1 && kind != 2) {
			continue
		}
		calendar := feed.Calendars[id]
		if calendar.Exceptions == nil {
			calendar.Exceptions = map[string]int{}
		}
		calendar.Exceptions[date] = kind
		feed.Calendars[id] = calendar
	}
	for _, row := range stops {
		lat, e1 := strconv.ParseFloat(row["stop_lat"], 64)
		lon, e2 := strconv.ParseFloat(row["stop_lon"], 64)
		v := busStop{ID: row["stop_id"], Name: row["stop_name"], Code: row["stop_code"], Lat: lat, Lon: lon, Enabled: true, Origin: "gtfs"}
		if e1 != nil || e2 != nil {
			continue
		}
		if err := validateStop(v); err != nil {
			continue
		}
		feed.Stops[v.ID] = v
	}
	for _, row := range routes {
		kind := row["route_type"]
		number, _ := strconv.Atoi(kind)
		if kind != "3" && (number < 700 || number > 799) {
			continue
		}
		name := row["route_long_name"]
		if name == "" {
			name = row["route_short_name"]
		}
		if name == "" {
			name = row["route_id"]
		}
		v := busRoute{ID: row["route_id"], Name: name, ShortName: row["route_short_name"], Type: "bus", Enabled: true, Origin: "gtfs"}
		if v.ID != "" {
			feed.Routes[v.ID] = v
		}
	}
	tripByID := map[string]busTrip{}
	for _, row := range trips {
		if _, ok := feed.Routes[row["route_id"]]; !ok {
			continue
		}
		tripByID[row["trip_id"]] = busTrip{RouteID: row["route_id"], ServiceID: row["service_id"], Headsign: row["trip_headsign"]}
	}
	type timedStop struct {
		Row      map[string]string
		Sequence int
	}
	byTrip := map[string][]timedStop{}
	for _, row := range times {
		if _, ok := tripByID[row["trip_id"]]; !ok {
			continue
		}
		if _, ok := feed.Stops[row["stop_id"]]; !ok {
			continue
		}
		sequence, err := strconv.Atoi(row["stop_sequence"])
		if err != nil || sequence < 1 {
			continue
		}
		byTrip[row["trip_id"]] = append(byTrip[row["trip_id"]], timedStop{Row: row, Sequence: sequence})
	}
	feed.Trips = len(byTrip)
	tripIDs := make([]string, 0, len(byTrip))
	for id := range byTrip {
		tripIDs = append(tripIDs, id)
	}
	sort.Strings(tripIDs)
	routeWithPath := map[string]bool{}
	for _, tripID := range tripIDs {
		trip := tripByID[tripID]
		rows := byTrip[tripID]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Sequence < rows[j].Sequence })
		path := make([][2]float64, 0, len(rows))
		for _, item := range rows {
			row := item.Row
			arrival := row["arrival_time"]
			departure := row["departure_time"]
			if !validBusTime(arrival) || !validBusTime(departure) {
				continue
			}
			stop := feed.Stops[row["stop_id"]]
			id := "gtfs:" + tripID + ":" + strconv.Itoa(item.Sequence) + ":" + stop.ID
			feed.Schedule[id] = busSchedule{ID: id, RouteID: trip.RouteID, TripID: tripID, Headsign: trip.Headsign, StopID: stop.ID, StopName: stop.Name, Sequence: item.Sequence, Arrival: arrival, Departure: departure, ServiceID: trip.ServiceID, Origin: "gtfs"}
			path = append(path, [2]float64{stop.Lat, stop.Lon})
		}
		if !routeWithPath[trip.RouteID] && len(path) > 1 {
			route := feed.Routes[trip.RouteID]
			route.Path = path
			feed.Routes[trip.RouteID] = route
			routeWithPath[trip.RouteID] = true
		}
	}
	if len(feed.Stops) == 0 || len(feed.Routes) == 0 || len(feed.Schedule) == 0 {
		return feed, errors.New("GTFS archive has no usable bus stops, routes, or stop times")
	}
	return feed, nil
}
