package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

func TestPublicDashboardDataOnlyReturnsConfiguredTags(t *testing.T) {
	db := securityTestDB(t, false)
	ops := tree.NewTreeWithOperations(nil)
	for path, value := range map[string]string{
		"default.device.visible": "PUBLIC_VALUE",
		"default.device.secret":  "PRIVATE_VALUE",
	} {
		if err := ops.CreateTag(path, tree.TypeString, tree.TagConfig{}); err != nil {
			t.Fatal(err)
		}
		if err := ops.SetLeafValue(path, value); err != nil {
			t.Fatal(err)
		}
	}
	dashboard := &sqldb.Dashboard{
		Name: "Public metrics", IsPublic: true,
		Widgets: json.RawMessage(`[{"id":"v","type":"big-number-widget","x":0,"y":0,"w":6,"h":5,"config":{"tagPath":"device.visible"}}]`),
	}
	if err := db.CreateDashboard(context.Background(), "default", dashboard); err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{ProxyPath: "/xact"}, ops, nil, nil, "test-secret", db, "")
	path := fmt.Sprintf("/xact/api/v1/public/default/dashboards/%d/data", dashboard.ID)
	response := securityRequest(server, "GET", path, "", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "PUBLIC_VALUE") || strings.Contains(response.Body.String(), "PRIVATE_VALUE") {
		t.Fatalf("public data: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public data must not be cached")
	}

	dashboard.IsPublic = false
	if err := db.UpdateDashboard(context.Background(), "default", dashboard.ID, dashboard); err != nil {
		t.Fatal(err)
	}
	response = securityRequest(server, "GET", path, "", nil)
	if response.Code != 404 {
		t.Fatalf("revoked public data = %d", response.Code)
	}
}

func TestPublicMapUsesEachBusHeadingAndKeepsLabelSetting(t *testing.T) {
	db := securityTestDB(t, false)
	ops := tree.NewTreeWithOperations(nil)
	for path, value := range map[string]float64{
		"default.PUBLIC_BUS.BUS-01.meta.lat":         15.30,
		"default.PUBLIC_BUS.BUS-01.meta.lon":         -61.40,
		"default.PUBLIC_BUS.BUS-01.meta.orientation": 10,
		"default.PUBLIC_BUS.BUS-17.meta.lat":         15.31,
		"default.PUBLIC_BUS.BUS-17.meta.lon":         -61.41,
		"default.PUBLIC_BUS.BUS-17.meta.orientation": 340,
	} {
		if err := ops.CreateTag(path, tree.TypeFloat, tree.TagConfig{}); err != nil {
			t.Fatal(err)
		}
		if err := ops.SetLeafValue(path, value); err != nil {
			t.Fatal(err)
		}
	}
	dashboard := &sqldb.Dashboard{
		Name: "Public buses", IsPublic: true,
		Widgets: json.RawMessage(`[{"id":"map","type":"area-map-widget","x":0,"y":0,"w":12,"h":12,"config":{"layers":[{"id":"buses","enabled":true,"itemType":"icon","pathPattern":"PUBLIC_BUS.*","iconRotationEnabled":true,"iconRotationTag":"PUBLIC_BUS.BUS-01.meta.orientation","showZoomedTooltipAlways":true,"positionAnimationMs":8000,"clusteringEnabled":false,"zoomWidgetType":"html-widget"}]}}]`),
	}
	if err := db.CreateDashboard(context.Background(), "default", dashboard); err != nil {
		t.Fatal(err)
	}
	public, err := sqldb.PublicDashboard(dashboard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(public.Widgets), `"clusteringEnabled":false`) || !strings.Contains(string(public.Widgets), `"positionAnimationMs":8000`) || !strings.Contains(string(public.Widgets), `"showZoomedTooltipAlways":true`) || strings.Contains(string(public.Widgets), "html-widget") {
		t.Fatalf("public layer config = %s", public.Widgets)
	}

	server := NewServer(ServerConfig{ProxyPath: "/xact"}, ops, nil, nil, "test-secret", db, "")
	path := fmt.Sprintf("/xact/api/v1/public/default/dashboards/%d/data", dashboard.ID)
	response := securityRequest(server, "GET", path, "", nil)
	if response.Code != 200 {
		t.Fatalf("public data: %d %s", response.Code, response.Body.String())
	}
	var values map[string]publicTagValue
	if err := json.Unmarshal(response.Body.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]float64{
		"default.PUBLIC_BUS.BUS-01.meta.orientation": 10,
		"default.PUBLIC_BUS.BUS-17.meta.orientation": 340,
	} {
		got, ok := values[path]
		if !ok || got.Value != want {
			t.Fatalf("%s = %#v, want %v", path, got, want)
		}
	}
}

func TestPublicRoutesReturnOnlyConfiguredGeometryAndNames(t *testing.T) {
	db := securityTestDB(t, false)
	ops := tree.NewTreeWithOperations(nil)
	for path, value := range map[string]string{
		"default.Routes.A.route.name":        "Route A",
		"default.Custom.B.shape.points":      "[15.4,-61.5,15.41,-61.51]",
		"default.Custom.B.meta.name":         "Route B",
		"default.Hidden.C.route.coordinates": "PRIVATE_GEOMETRY",
		"default.Routes.A.secret":            "PRIVATE_VALUE",
	} {
		if err := ops.CreateTag(path, tree.TypeString, tree.TagConfig{}); err != nil {
			t.Fatal(err)
		}
		if err := ops.SetLeafValue(path, value); err != nil {
			t.Fatal(err)
		}
	}
	for i, value := range []float64{15.3, -61.4, 15.31, -61.41} {
		path := fmt.Sprintf("default.Routes.A.route.coordinates.%d", i)
		if err := ops.CreateTag(path, tree.TypeFloat, tree.TagConfig{}); err != nil {
			t.Fatal(err)
		}
		if err := ops.SetLeafValue(path, value); err != nil {
			t.Fatal(err)
		}
	}
	array, err := ops.FindNode("default.Routes.A.route.coordinates")
	if err != nil {
		t.Fatal(err)
	}
	array.SetIsArray(true)
	dashboard := &sqldb.Dashboard{Name: "Routes", IsPublic: true, Widgets: json.RawMessage(`[{"id":"map","type":"area-map-widget","config":{"layers":[{"itemType":"route","pathPattern":"Routes.*","enabled":true},{"itemType":"route","pathPattern":"Custom.*","enabled":true,"routeCoordinatesTag":"shape.points","routeNameTag":"meta.name"},{"itemType":"route","pathPattern":"Hidden.*","enabled":false}]}}]`)}
	if err := db.CreateDashboard(context.Background(), "default", dashboard); err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{ProxyPath: "/xact"}, ops, nil, nil, "test-secret", db, "")
	response := securityRequest(server, "GET", fmt.Sprintf("/xact/api/v1/public/default/dashboards/%d/data", dashboard.ID), "", nil)
	if response.Code != 200 {
		t.Fatalf("public routes: %d %s", response.Code, response.Body.String())
	}
	var values map[string]publicTagValue
	if err := json.Unmarshal(response.Body.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"default.Routes.A.route.coordinates", "default.Routes.A.route.name", "default.Custom.B.shape.points", "default.Custom.B.meta.name"} {
		if _, ok := values[path]; !ok {
			t.Errorf("missing %s", path)
		}
	}
	if len(values) != 4 {
		t.Fatalf("unexpected public route tags: %s", response.Body.String())
	}
	coordinates, ok := values["default.Routes.A.route.coordinates"].Value.([]any)
	if !ok || len(coordinates) != 4 || coordinates[0] != 15.3 || coordinates[3] != -61.41 {
		t.Fatalf("native coordinate array = %#v", values["default.Routes.A.route.coordinates"].Value)
	}
}
