package tagcalcs

import (
	"strings"
	"testing"
)

// ── preprocess ────────────────────────────────────────────────────────────────

func TestPreprocess_SingleTagReference(t *testing.T) {
	got := preprocess("VMS.Sign001.brightness")
	want := `Tag("VMS.Sign001.brightness")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreprocess_TagReferenceInArithmetic(t *testing.T) {
	got := preprocess("VMS.Sign001.temp + VMS.Sign002.temp")
	want := `Tag("VMS.Sign001.temp") + Tag("VMS.Sign002.temp")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreprocess_TagReferenceWithDivision(t *testing.T) {
	got := preprocess("Pump.output / Pump.input")
	want := `Tag("Pump.output") / Tag("Pump.input")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreprocess_DeepPath(t *testing.T) {
	got := preprocess("VMS.Sign001.status.brightness")
	want := `Tag("VMS.Sign001.status.brightness")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreprocess_AggregateFunctions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"avg", `avg(VMS.*.brightness)`, `Avg("VMS.*.brightness")`},
		{"sum", `sum(VMS.*.errorCount)`, `Sum("VMS.*.errorCount")`},
		{"min", `min(Tanks.*.level)`, `AggMin("Tanks.*.level")`},
		{"max", `max(Pumps.*.motorTemp)`, `AggMax("Pumps.*.motorTemp")`},
		{"count", `count(VMS.*.meta.name)`, `Count("VMS.*.meta.name")`},
		{"AVG uppercase", `AVG(VMS.*.brightness)`, `Avg("VMS.*.brightness")`},
		{"SUM uppercase", `SUM(VMS.*.brightness)`, `Sum("VMS.*.brightness")`},
		{"Count mixed case", `Count(VMS.*)`, `Count("VMS.*")`},
		{"deep wildcard", `avg(VMS.*.meta.online)`, `Avg("VMS.*.meta.online")`},
		{"question mark wildcard", `count(VMS.Sign00?.brightness)`, `Count("VMS.Sign00?.brightness")`},
		{"listHighest", `listHighest(LA_LongBeach.AirQuality.*.air.aqi, 5)`, `ListHighest("LA_LongBeach.AirQuality.*.air.aqi", 5)`},
		{"listLowest", `listLowest(LA_LongBeach.AirQuality.*.air.aqi, 5)`, `ListLowest("LA_LongBeach.AirQuality.*.air.aqi", 5)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := preprocess(tc.in)
			if got != tc.want {
				t.Errorf("preprocess(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPreprocess_CountWhere(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"numeric zero", `countWhere(VMS.*.alarm, 0)`, `CountWhere("VMS.*.alarm", 0)`},
		{"numeric one", `countWhere(VMS.*.alarm, 1)`, `CountWhere("VMS.*.alarm", 1)`},
		{"numeric float", `countWhere(Pumps.*.stage, 2.5)`, `CountWhere("Pumps.*.stage", 2.5)`},
		{"false literal", `countWhere(VMS.*.online, false)`, `CountWhere("VMS.*.online", 0)`},
		{"true literal", `countWhere(VMS.*.online, true)`, `CountWhere("VMS.*.online", 1)`},
		{"FALSE uppercase", `countWhere(VMS.*.online, FALSE)`, `CountWhere("VMS.*.online", 0)`},
		{"TRUE uppercase", `countWhere(VMS.*.online, TRUE)`, `CountWhere("VMS.*.online", 1)`},
		{"string single quotes", `countWhere(Singapore.Incidents.*.kpi.type, 'Accident')`, `CountWhere("Singapore.Incidents.*.kpi.type", 'Accident')`},
		{"string double quotes", `countWhere(Singapore.Incidents.*.kpi.type, "Accident")`, `CountWhere("Singapore.Incidents.*.kpi.type", "Accident")`},
		{"with spaces", `countWhere( VMS.*.alarm , false )`, `CountWhere("VMS.*.alarm", 0)`},
		{"COUNTWHERE uppercase", `COUNTWHERE(VMS.*.alarm, 0)`, `CountWhere("VMS.*.alarm", 0)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := preprocess(tc.in)
			if got != tc.want {
				t.Errorf("preprocess(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPreprocess_TagInsideStringNotRewritten(t *testing.T) {
	// A path inside a quoted string (already processed by an aggregate) must
	// not be wrapped in a second Tag() call.
	in := `Avg("VMS.*.brightness")`
	got := preprocess(in)
	if strings.Contains(got, `Tag(`) {
		t.Errorf("preprocess rewrote a path inside a string literal: %q", got)
	}
	if got != in {
		t.Errorf("preprocess modified a fully-preprocessed expression: got %q, want %q", got, in)
	}
}

func TestPreprocess_FunctionNameNotRewritten(t *testing.T) {
	// Function names that start with a dot-path-like identifier must not be
	// wrapped: e.g. "avg" in "avg(VMS.*)" is followed by "(" so it stays.
	got := preprocess("avg(VMS.*.brightness)")
	if strings.Contains(got, `Tag("avg`) {
		t.Errorf("function name was incorrectly wrapped in Tag(): %q", got)
	}
}

func TestPreprocess_MixedTagsAndAggregates(t *testing.T) {
	in := "avg(VMS.*.brightness) + VMS.Sign001.brightness"
	got := preprocess(in)
	if !strings.Contains(got, `Avg("VMS.*.brightness")`) {
		t.Errorf("aggregate not rewritten: %q", got)
	}
	if !strings.Contains(got, `Tag("VMS.Sign001.brightness")`) {
		t.Errorf("tag reference not rewritten: %q", got)
	}
}

func TestPreprocess_ComplexExpression(t *testing.T) {
	in := `round(countWhere(VMS.*.meta.online, true) / max(1, count(VMS.*.meta.online)) * 100, 1)`
	got := preprocess(in)
	if !strings.Contains(got, `CountWhere("VMS.*.meta.online", 1)`) {
		t.Errorf("countWhere not rewritten correctly: %q", got)
	}
	if !strings.Contains(got, `Count("VMS.*.meta.online")`) {
		t.Errorf("count not rewritten correctly: %q", got)
	}
	// The literal 1 and 100 should not be rewritten
	if strings.Contains(got, `Tag("1`) {
		t.Errorf("numeric literal was incorrectly wrapped: %q", got)
	}
}

func TestPreprocess_NumbersNotRewritten(t *testing.T) {
	got := preprocess("42 + 3.14 * 100")
	if strings.Contains(got, "Tag(") {
		t.Errorf("numeric literals were incorrectly wrapped: %q", got)
	}
}

// ── compileExpression ─────────────────────────────────────────────────────────

func TestCompileExpression_ValidExpressions(t *testing.T) {
	cases := []string{
		"VMS.Sign001.brightness",
		"VMS.Sign001.brightness + 10",
		"avg(VMS.*.brightness)",
		"sum(VMS.*.brightness) / max(1, count(VMS.*.brightness))",
		"countWhere(VMS.*.online, true)",
		"if(VMS.Sign001.brightness > 50, 1, 0)",
		"round(avg(VMS.*.brightness), 2)",
		"abs(Pump.deviation)",
		"sqrt(pow(A.x, 2) + pow(A.y, 2))",
		"if(Tank.level < 20 && Pump.running == 0, 1, 0)",
		"listHighest(LA_LongBeach.AirQuality.*.air.aqi, 5)",
		"listLowest(LA_LongBeach.AirQuality.*.air.aqi, 5)",
		"42",
		"3.14 * pow(Tank.radius, 2)",
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			prog, err := compileExpression(expr)
			if err != nil {
				t.Errorf("compileExpression(%q) returned unexpected error: %v", expr, err)
			}
			if prog == nil {
				t.Errorf("compileExpression(%q) returned nil program", expr)
			}
		})
	}
}

func TestCompileExpression_InvalidSyntax(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"unclosed paren", "avg(VMS.*.brightness"},
		{"bare plus", "+"},
		{"incomplete if", "if(A.x > 0, 1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileExpression(tc.expr)
			if err == nil {
				t.Errorf("compileExpression(%q) expected error, got nil", tc.expr)
			}
		})
	}
}

// ── normaliseResult ───────────────────────────────────────────────────────────

func TestNormaliseResult(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    float64
		wantErr bool
	}{
		{"float64", float64(3.14), 3.14, false},
		{"float32", float32(2.5), 2.5, false},
		{"int", int(7), 7.0, false},
		{"int64", int64(42), 42.0, false},
		{"bool true", true, 1.0, false},
		{"bool false", false, 0.0, false},
		{"string unsupported", "hello", 0, true},
		{"nil unsupported", nil, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseResult(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("normaliseResult(%v) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normaliseResult(%v) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("normaliseResult(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
