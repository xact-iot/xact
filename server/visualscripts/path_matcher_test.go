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

func TestTagChangeRouterSeparatesTenantsAndLimitsFanout(t *testing.T) {
	router := NewTagChangeRouter(10, 2)
	calls := 0
	register := func(org, id, pattern string) {
		if _, err := router.Register(org, id, pattern, func(TagChange) { calls++ }); err != nil {
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
	if matched := router.Dispatch(TagChange{OrgName: "missing", TagPath: "SITE.Pump01.Value"}); matched != 0 {
		t.Fatalf("tenant leak matched %d", matched)
	}
}
