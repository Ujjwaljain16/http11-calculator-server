package validate_test

import (
	"math"
	"strconv"
	"testing"

	"calcserver/internal/httpmsg"
	"calcserver/internal/validate"
)

// Param aliases validate.Param so test tables can write Param{...} tersely.
type Param = validate.Param

func parseOrFatal(t *testing.T, raw string) httpmsg.Request {
	t.Helper()
	req, err := httpmsg.ParseRequest([]byte(raw))
	if err != nil {
		t.Fatalf("ParseRequest(%q): %v", raw, err)
	}
	return req
}

func TestValidate_Host(t *testing.T) {
	cases := map[string]struct {
		raw      string
		wantHost bool
	}{
		"present":                 {"GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n", true},
		"missing":                 {"GET /add?a=1&b=2 HTTP/1.1\r\n\r\n", false},
		"present_but_empty_value": {"GET /add?a=1&b=2 HTTP/1.1\r\nHost:\r\n\r\n", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := validate.Validate(parseOrFatal(t, c.raw)).HostOK
			if got != c.wantHost {
				t.Errorf("HostOK = %v, want %v", got, c.wantHost)
			}
		})
	}
}

func TestValidate_Params(t *testing.T) {
	cases := []struct {
		name  string
		query string
		wantA Param
		wantB Param
	}{
		{"valid_both", "a=2&b=3", Param{true, 2}, Param{true, 3}},
		{"missing_a", "b=3", Param{}, Param{true, 3}},
		{"missing_b", "a=2", Param{true, 2}, Param{}},
		{"both_missing", "", Param{}, Param{}},
		{"empty_a", "a=&b=3", Param{}, Param{true, 3}},
		{"empty_b", "a=2&b=", Param{true, 2}, Param{}},
		{"non_numeric_a", "a=x&b=3", Param{}, Param{true, 3}},
		{"non_numeric_b", "a=2&b=y", Param{true, 2}, Param{}},
		{"decimal_is_not_integer", "a=1.5&b=3", Param{}, Param{true, 3}},
		{"negative_integers", "a=-5&b=-10", Param{true, -5}, Param{true, -10}},
		{"zero", "a=0&b=0", Param{true, 0}, Param{true, 0}},
		{"large_valid_integer", "a=" + strconv.FormatInt(math.MaxInt64, 10) + "&b=1", Param{true, math.MaxInt64}, Param{true, 1}},
		{"parsing_overflow", "a=9223372036854775808&b=1", Param{}, Param{true, 1}},
		{"duplicate_keeps_first", "a=1&a=2&b=3", Param{true, 1}, Param{true, 3}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := parseOrFatal(t, "GET /add?"+c.query+" HTTP/1.1\r\nHost: x\r\n\r\n")
			result := validate.Validate(req)
			if result.A != c.wantA {
				t.Errorf("A = %+v, want %+v", result.A, c.wantA)
			}
			if result.B != c.wantB {
				t.Errorf("B = %+v, want %+v", result.B, c.wantB)
			}
		})
	}
}

func TestValidate_OK(t *testing.T) {
	cases := map[string]struct {
		raw    string
		wantOK bool
	}{
		"valid":     {"GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n", true},
		"bad_host":  {"GET /add?a=1&b=2 HTTP/1.1\r\n\r\n", false},
		"bad_param": {"GET /add?a=x&b=2 HTTP/1.1\r\nHost: x\r\n\r\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := validate.Validate(parseOrFatal(t, c.raw)).OK()
			if got != c.wantOK {
				t.Errorf("OK() = %v, want %v", got, c.wantOK)
			}
		})
	}
}

// Validation must not encode any routing or method decision: an unsupported
// operation and a disallowed method both validate exactly as a supported
// GET request would - only Router (Phase 5) decides what to do about the
// path/method themselves.
func TestValidate_DoesNotEncodeRoutingOrMethodDecisions(t *testing.T) {
	unsupportedOp := parseOrFatal(t, "GET /pow?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n")
	if !validate.Validate(unsupportedOp).OK() {
		t.Errorf("/pow with valid host+params must validate OK; unsupported operation is routing's concern")
	}

	wrongMethod := parseOrFatal(t, "POST /add?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n")
	if !validate.Validate(wrongMethod).OK() {
		t.Errorf("POST with valid host+params must validate OK; method is routing's concern")
	}

	mixedValidity := parseOrFatal(t, "GET /add?a=x&b=3 HTTP/1.1\r\nHost: x\r\n\r\n")
	result := validate.Validate(mixedValidity)
	if result.A.OK {
		t.Errorf("expected a=x to fail parameter validation")
	}
	if result.B != (Param{true, 3}) {
		t.Errorf("expected b=3 to still validate correctly, got %+v", result.B)
	}
}
