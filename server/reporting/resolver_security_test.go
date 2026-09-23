package reporting

import (
	"context"
	"testing"
)

func TestReportTagReaderIsTenantScoped(t *testing.T) {
	for _, tc := range []struct {
		org, path string
		allowed   bool
	}{
		{"alpha", "alpha.device.tag", true},
		{"alpha", "/alpha/device/tag", true},
		{"alpha", "alpha/device.tag", true},
		{"alpha", "beta.device.tag", false},
		{"alpha", "/beta/device/tag", false},
		{"alpha", "alphabet.device.tag", false},
		{"alpha", "alpha/../beta/device/tag", false},
		{"alpha", "alpha..device.tag", false},
		{"alpha", "//alpha/device/tag", false},
		{"alpha", "alpha.*.tag", false},
		{"alpha", "alpha\\device\\tag", false},
		{"", "alpha.device.tag", false},
		{"alpha/beta", "alpha/beta/device/tag", false},
	} {
		calls := 0
		values := ResolveVariables(context.Background(), []Variable{{Name: "value", Type: VariableTypeRTDB, Path: tc.path}}, ResolveContext{OrgName: tc.org, TagReader: func(path string) (string, bool) {
			calls++
			if path != "alpha.device.tag" {
				t.Errorf("unexpected reader path %q", path)
			}
			return "private", true
		}})
		if (calls == 1) != tc.allowed || (values["value"] == "private") != tc.allowed {
			t.Errorf("org=%q path=%q: calls=%d values=%v", tc.org, tc.path, calls, values)
		}
	}
}
