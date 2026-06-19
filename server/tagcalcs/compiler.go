package tagcalcs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// tagRefRe matches dot-separated identifiers that look like tag paths,
// e.g. SYSTEM.LoopTime.Current. Go's RE2 has no lookahead, so we check
// the character that follows each match in preprocess() instead.
var tagRefRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_*?]*(?:\.[A-Za-z0-9_*?]+)+`)

// aggregateRe matches aggregate function calls whose argument is a bare
// (unquoted) tag path pattern, e.g. avg(SITE.*.temp) or count(LTA.VMS.*).
// Group 1 = function name, group 2 = pattern argument.
// Note: max/min are NOT included - they are handled by aggMaxMinRe below so
// that the two-arg forms max(a,b) and min(a,b) can map to the binary Max/Min.
var aggregateRe = regexp.MustCompile(`(?i)(avg|sum|count)\(\s*([A-Za-z_][A-Za-z0-9_*?./]*)\s*\)`)

// aggMaxMinRe handles the aggregate forms max(pattern) and min(pattern) where
// the single argument is a bare wildcard path. These are rewritten to AggMax/AggMin
// so that the two-arg binary forms max(a,b) and min(a,b) can coexist in the env.
var aggMaxMinRe = regexp.MustCompile(`(?i)(max|min)\(\s*([A-Za-z_][A-Za-z0-9_*?./]*)\s*\)`)

// countWhereRe matches countWhere(path.pattern, value) calls where the first
// argument is a bare tag path pattern and the second is a numeric literal.
// Group 1 = pattern, group 2 = value literal.
var countWhereRe = regexp.MustCompile(`(?i)countWhere\(\s*([A-Za-z_][A-Za-z0-9_*?./]*)\s*,\s*([^)]+?)\s*\)`)

// listRe matches listHighest/listLowest(path.pattern, count) calls where the
// first argument is a bare tag path pattern.
var listRe = regexp.MustCompile(`(?i)(listHighest|listLowest)\(\s*([A-Za-z_][A-Za-z0-9_*?./]*)\s*,\s*([^)]+?)\s*\)`)

// ifRe matches the if keyword followed by ( so it can be capitalised to If(
// before expr sees it as a keyword.
var ifRe = regexp.MustCompile(`(?i)\bif\(`)

// binaryMaxRe / binaryMinRe capitalise any remaining max/min (i.e. the binary
// two-arg forms) after the single-arg aggregate forms have been rewritten.
var binaryMaxRe = regexp.MustCompile(`(?i)\bmax\(`)
var binaryMinRe = regexp.MustCompile(`(?i)\bmin\(`)

// mathFnRe matches math function calls by name so they can be normalised to
// their capitalised Go method names (Abs, Round, …).  log10 must appear before
// log so the longer name is matched first.
var mathFnRe = regexp.MustCompile(`(?i)\b(abs|round|floor|ceil|sqrt|pow|log10|log|sin|cos|tan)\(`)
var mathFnCapMap = map[string]string{
	"abs": "Abs(", "round": "Round(", "floor": "Floor(", "ceil": "Ceil(",
	"sqrt": "Sqrt(", "pow": "Pow(", "log10": "Log10(", "log": "Log(",
	"sin": "Sin(", "cos": "Cos(", "tan": "Tan(",
}

// insideQuotes returns a bool slice where true means that byte position is
// inside a double-quoted string in s.
func insideQuotes(s string) []bool {
	inside := make([]bool, len(s))
	inQuote := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			inQuote = !inQuote
		} else if inQuote {
			inside[i] = true
		}
	}
	return inside
}

// capitalise returns s with its first character upper-cased.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// preprocess replaces bare tag references with Tag("...") calls and rewrites
// aggregate calls so they compile correctly against the custom env methods.
//
// Processing order matters:
//  1. Aggregate calls with bare pattern args are rewritten first:
//     count(LTA.VMS.*.meta.name) → Count("LTA.VMS.*.meta.name")
//     avg(SITE.*.temp)           → Avg("SITE.*.temp")
//  2. Remaining bare dot-paths are wrapped with Tag():
//     SYSTEM.LoopTime.Current    → Tag("SYSTEM.LoopTime.Current")
func preprocess(raw string) string {
	// Step 1a: rewrite countWhere(bare.path, val) → CountWhere("bare.path", val)
	// Boolean literals true/false are normalised to 1/0.
	raw = countWhereRe.ReplaceAllStringFunc(raw, func(match string) string {
		sub := countWhereRe.FindStringSubmatch(match)
		val := strings.TrimSpace(sub[2])
		switch strings.ToLower(val) {
		case "true":
			val = "1"
		case "false":
			val = "0"
		}
		return `CountWhere("` + sub[1] + `", ` + val + `)`
	})

	// Step 1b: rewrite listHighest/listLowest(bare.path, count) with a quoted
	// pattern argument.
	raw = listRe.ReplaceAllStringFunc(raw, func(match string) string {
		sub := listRe.FindStringSubmatch(match)
		name := strings.ToLower(sub[1])
		if name == "listhighest" {
			name = "ListHighest"
		} else {
			name = "ListLowest"
		}
		return name + `("` + sub[2] + `", ` + strings.TrimSpace(sub[3]) + `)`
	})

	// Step 1c: rewrite aggregate max/min(bare.path) → AggMax/AggMin("bare.path")
	raw = aggMaxMinRe.ReplaceAllStringFunc(raw, func(match string) string {
		sub := aggMaxMinRe.FindStringSubmatch(match)
		return "Agg" + capitalise(sub[1]) + `("` + sub[2] + `")`
	})

	// Step 1d: rewrite avg/sum/count(bare.path) → Avg/Sum/Count("bare.path")
	raw = aggregateRe.ReplaceAllStringFunc(raw, func(match string) string {
		sub := aggregateRe.FindStringSubmatch(match)
		return capitalise(sub[1]) + `("` + sub[2] + `")`
	})

	// Step 1e: convert if( → If( so it is treated as a function call rather
	// than the expr keyword.
	raw = ifRe.ReplaceAllStringFunc(raw, func(_ string) string { return "If(" })

	// Step 1f: capitalise remaining binary max(a,b) / min(a,b).
	// All aggregate forms (single bare-path arg) were already rewritten to
	// AggMax/AggMin in step 1b, so any remaining max/min must be binary.
	// \b ensures AggMax( is not re-matched (no word boundary before 'max' in AggMax).
	raw = binaryMaxRe.ReplaceAllStringFunc(raw, func(_ string) string { return "Max(" })
	raw = binaryMinRe.ReplaceAllStringFunc(raw, func(_ string) string { return "Min(" })

	// Step 1g: capitalise math function names (abs, round, floor, …) so they
	// resolve to the env methods Abs, Round, Floor, …
	raw = mathFnRe.ReplaceAllStringFunc(raw, func(match string) string {
		name := strings.ToLower(strings.TrimSuffix(match, "("))
		if cap, ok := mathFnCapMap[name]; ok {
			return cap
		}
		return match
	})

	// Step 2: wrap remaining bare tag paths in Tag("..."), skipping anything
	// already inside a quoted string (e.g. patterns rewritten in step 1).
	quoted := insideQuotes(raw)
	matches := tagRefRe.FindAllStringIndex(raw, -1)
	if len(matches) == 0 {
		return raw
	}
	var sb strings.Builder
	last := 0
	for _, m := range matches {
		if quoted[m[0]] {
			continue // already inside a string literal - leave untouched
		}
		sb.WriteString(raw[last:m[0]])
		identifier := raw[m[0]:m[1]]
		// If the identifier is immediately followed by optional whitespace then
		// '(' it is a function call - leave it untouched.
		rest := strings.TrimLeft(raw[m[1]:], " \t")
		if len(rest) > 0 && rest[0] == '(' {
			sb.WriteString(identifier)
		} else {
			sb.WriteString(fmt.Sprintf(`Tag("%s")`, identifier))
		}
		last = m[1]
	}
	sb.WriteString(raw[last:])
	return sb.String()
}

// compileExpression pre-processes and compiles a tag calc expression.
// Returns the bytecode program ready for repeated evaluation.
func compileExpression(raw string) (*vm.Program, error) {
	processed := preprocess(raw)
	prog, err := expr.Compile(processed,
		expr.Env(evalEnv{}),
		expr.AllowUndefinedVariables(),
	)
	if err != nil {
		return nil, fmt.Errorf("compile expression %q: %w", raw, err)
	}
	return prog, nil
}

// evalEnv is the type-checked environment passed to expr at compile time.
// At runtime we use a concrete *runtimeEnv that implements the same methods.
type evalEnv struct{}

func (evalEnv) Tag(path string) float64                      { return 0 }
func (evalEnv) Avg(pattern string) float64                   { return 0 }
func (evalEnv) Sum(pattern string) float64                   { return 0 }
func (evalEnv) AggMin(pattern string) float64                { return 0 }
func (evalEnv) AggMax(pattern string) float64                { return 0 }
func (evalEnv) Count(pattern string) float64                 { return 0 }
func (evalEnv) CountWhere(pattern string, value any) float64 { return 0 }
func (evalEnv) ListHighest(pattern string, count int) []ListEntry {
	return nil
}
func (evalEnv) ListLowest(pattern string, count int) []ListEntry {
	return nil
}
func (evalEnv) Max(a, b float64) float64 { return 0 }
func (evalEnv) Min(a, b float64) float64 { return 0 }

// Math stubs for compile-time type checking
func (evalEnv) Abs(v float64) float64               { return 0 }
func (evalEnv) Round(v float64, dec int) float64    { return 0 }
func (evalEnv) Sqrt(v float64) float64              { return 0 }
func (evalEnv) Pow(base, exp float64) float64       { return 0 }
func (evalEnv) Floor(v float64) float64             { return 0 }
func (evalEnv) Ceil(v float64) float64              { return 0 }
func (evalEnv) Log(v float64) float64               { return 0 }
func (evalEnv) Log10(v float64) float64             { return 0 }
func (evalEnv) Sin(v float64) float64               { return 0 }
func (evalEnv) Cos(v float64) float64               { return 0 }
func (evalEnv) Tan(v float64) float64               { return 0 }
func (evalEnv) If(cond bool, a, b float64) float64  { return 0 }
func (evalEnv) IfStr(cond bool, a, b string) string { return "" }

// normaliseResult coerces the expression output to float64.
// expr may return int, float32, float64, or bool depending on the expression.
func normaliseResult(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("expression returned unsupported type %T", v)
	}
}

// preprocessedExpression returns the pre-processed form (for display / debugging).
func preprocessedExpression(raw string) string {
	return strings.TrimSpace(preprocess(raw))
}
