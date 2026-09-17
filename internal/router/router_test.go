package router_test

import (
	"testing"

	"calcserver/internal/httpmsg"
	"calcserver/internal/router"
	"calcserver/internal/validate"
)

// route parses raw (reusing the already-tested parser and validator as
// fixtures, not re-testing them) and runs it through Router.
func route(t *testing.T, raw string) router.Decision {
	t.Helper()
	req, err := httpmsg.ParseRequest([]byte(raw))
	if err != nil {
		t.Fatalf("ParseRequest(%q): %v", raw, err)
	}
	return router.Route(req, validate.Validate(req))
}

func TestRoute_KnownOperationsSucceed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"add", "GET /add?a=2&b=3 HTTP/1.1\r\nHost: x\r\n\r\n", 5},
		{"sub", "GET /sub?a=10&b=4 HTTP/1.1\r\nHost: x\r\n\r\n", 6},
		{"mul", "GET /mul?a=6&b=7 HTTP/1.1\r\nHost: x\r\n\r\n", 42},
		{"div", "GET /div?a=9&b=3 HTTP/1.1\r\nHost: x\r\n\r\n", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := route(t, c.raw)
			if d.Outcome != router.OutcomeOK {
				t.Fatalf("Outcome = %v, want OutcomeOK", d.Outcome)
			}
			if d.Result != c.want {
				t.Errorf("Result = %d, want %d", d.Result, c.want)
			}
		})
	}
}

func TestRoute_UnknownPathIsNotFound(t *testing.T) {
	cases := map[string]string{
		"unsupported_operation": "GET /pow?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n",
		"entirely_unknown_path": "GET /status?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if d := route(t, raw); d.Outcome != router.OutcomeNotFound {
				t.Errorf("Outcome = %v, want OutcomeNotFound", d.Outcome)
			}
		})
	}
}

func TestRoute_KnownPathWrongMethodIsMethodNotAllowed(t *testing.T) {
	cases := map[string]string{
		"post_add":   "POST /add?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n",
		"put_sub":    "PUT /sub?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n",
		"delete_div": "DELETE /div?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if d := route(t, raw); d.Outcome != router.OutcomeMethodNotAllowed {
				t.Errorf("Outcome = %v, want OutcomeMethodNotAllowed", d.Outcome)
			}
		})
	}
}

// A wrong method must never be reported as NotFound, and an unknown path
// must never be reported as MethodNotAllowed - the two failure modes are
// tested together here so the distinction itself is what's verified.
func TestRoute_UnknownPathAndWrongMethodAreDistinct(t *testing.T) {
	unknownPath := route(t, "GET /pow?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n")
	if unknownPath.Outcome != router.OutcomeNotFound {
		t.Errorf("unknown path: Outcome = %v, want OutcomeNotFound", unknownPath.Outcome)
	}

	wrongMethod := route(t, "POST /add?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n")
	if wrongMethod.Outcome != router.OutcomeMethodNotAllowed {
		t.Errorf("known path, wrong method: Outcome = %v, want OutcomeMethodNotAllowed", wrongMethod.Outcome)
	}
}

func TestRoute_BadRequestCases(t *testing.T) {
	cases := map[string]string{
		"division_by_zero":  "GET /div?a=9&b=0 HTTP/1.1\r\nHost: x\r\n\r\n",
		"addition_overflow": "GET /add?a=9223372036854775807&b=1 HTTP/1.1\r\nHost: x\r\n\r\n",
		"non_numeric_param": "GET /add?a=x&b=3 HTTP/1.1\r\nHost: x\r\n\r\n",
		"missing_param":     "GET /add?a=1 HTTP/1.1\r\nHost: x\r\n\r\n",
		"missing_host":      "GET /add?a=1&b=2 HTTP/1.1\r\n\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if d := route(t, raw); d.Outcome != router.OutcomeBadRequest {
				t.Errorf("Outcome = %v, want OutcomeBadRequest", d.Outcome)
			}
		})
	}
}
