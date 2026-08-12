package visualscripts

import "testing"

func TestPathMatcherUsesSegmentWildcards(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"SITE.Pump01.Status.Running", "SITE/Pump01/Status/Running", true},
		{"SITE.*.Status.Running", "SITE.Pump01.Status.Running", true},
		{"SITE.*.Status.Running", "SITE.Area.Pump01.Status.Running", false},
		{"SITE.Pump??.Status.Running", "SITE.Pump01.Status.Running", true},
		{"SITE.Pump??.Status.Running", "SITE.Pump1.Status.Running", false},
		{"SITE.Pümp?.Value", "SITE.Pümp1.Value", true},
	}
	for _, test := range tests {
		matcher, err := CompilePathPattern(test.pattern)
		if err != nil {
			t.Fatal(err)
		}
		if got := matcher.Match(test.path); got != test.want {
			t.Errorf("%q matching %q = %v, want %v", test.pattern, test.path, got, test.want)
		}
	}
}

func TestPathMatcherDerivesStableWildcardInstanceKey(t *testing.T) {
	matcher, err := CompilePathPattern("SITE.Area*.Pump??.Status.Running")
	if err != nil {
		t.Fatal(err)
	}
	instanceKey, matched := matcher.MatchInstance("SITE.AreaWest.Pump01.Status.Running")
	if !matched || instanceKey != "AreaWest/Pump01" {
		t.Fatalf("instance match = %q, %v; want AreaWest/Pump01, true", instanceKey, matched)
	}
}

func TestTagChangeRouterSeparatesTenantsAndLimitsFanout(t *testing.T) {
	router := NewTagChangeRouter(10, 2)
	calls := 0
	instanceKeys := map[string]bool{}
	register := func(org, id, pattern string) {
		if _, err := router.Register(org, id, pattern, func(change TagChange) { calls++; instanceKeys[change.InstanceKey] = true }); err != nil {
			t.Fatal(err)
		}
	}
	register("a", "exact", "SITE.Pump01.Value")
	register("a", "wild-1", "SITE.*.Value")
	register("a", "wild-2", "SITE.Pump??.Value")
	register("b", "other", "SITE.*.Value")
	if matched := router.Dispatch(TagChange{OrgName: "a", TagPath: "SITE.Pump01.Value"}); matched != 2 {
		t.Fatalf("fanout=%d, want 2", matched)
	}
	if calls != 2 {
		t.Fatalf("callbacks=%d, want 2", calls)
	}
	if !instanceKeys["SITE.Pump01.Value"] || !instanceKeys["Pump01"] {
		t.Fatalf("callback instance keys = %#v", instanceKeys)
	}
	if matched := router.Dispatch(TagChange{OrgName: "missing", TagPath: "SITE.Pump01.Value"}); matched != 0 {
		t.Fatalf("tenant leak matched %d", matched)
	}
}
