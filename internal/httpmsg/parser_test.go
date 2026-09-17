package httpmsg_test

import (
	"errors"
	"testing"

	"calcserver/internal/httpmsg"
)

// Valid request line parsing.
func TestParseRequest_ValidRequestLine(t *testing.T) {
	req, err := httpmsg.ParseRequest([]byte("GET /add?a=2&b=3 HTTP/1.1\r\nHost: localhost\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "GET" {
		t.Errorf("Method = %q, want GET", req.Method)
	}
	if req.Target != "/add?a=2&b=3" {
		t.Errorf("Target = %q, want /add?a=2&b=3", req.Target)
	}
	if req.Version != "HTTP/1.1" {
		t.Errorf("Version = %q, want HTTP/1.1", req.Version)
	}
}

// Valid headers.
func TestParseRequest_ValidHeaders(t *testing.T) {
	req, err := httpmsg.ParseRequest([]byte(
		"GET /add?a=2&b=3 HTTP/1.1\r\nHost: localhost\r\nX-Custom: some value\r\nX-Empty:\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := req.Headers.Get("Host"); !ok || got != "localhost" {
		t.Errorf("Host = %q, ok=%v, want localhost, true", got, ok)
	}
	if got, ok := req.Headers.Get("X-Custom"); !ok || got != "some value" {
		t.Errorf("X-Custom = %q, ok=%v, want %q, true", got, ok, "some value")
	}
	if got, ok := req.Headers.Get("X-Empty"); !ok || got != "" {
		t.Errorf("X-Empty = %q, ok=%v, want empty string, true", got, ok)
	}
	if len(req.Headers) != 3 {
		t.Errorf("len(Headers) = %d, want 3", len(req.Headers))
	}
}

// Host extraction/presence representation - both when present and when
// absent. The parser never rejects a missing Host; it just reports it.
func TestParseRequest_HostPresenceRepresentation(t *testing.T) {
	withHost, err := httpmsg.ParseRequest([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: example\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := withHost.Headers.Get("Host"); !ok || got != "example" {
		t.Errorf("Host = %q, ok=%v, want example, true", got, ok)
	}

	withoutHost, err := httpmsg.ParseRequest([]byte("GET /add?a=1&b=2 HTTP/1.1\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error for request with no Host header: %v", err)
	}
	if _, ok := withoutHost.Headers.Get("Host"); ok {
		t.Errorf("expected no Host header to be present")
	}
}

// Path extraction.
func TestParseRequest_PathExtraction(t *testing.T) {
	cases := []struct {
		target   string
		wantPath string
	}{
		{"/", "/"},
		{"/add", "/add"},
		{"/add?a=1&b=2", "/add"},
		{"/add%20now?a=1", "/add now"}, // percent-decoded path
	}
	for _, c := range cases {
		req, err := httpmsg.ParseRequest([]byte("GET " + c.target + " HTTP/1.1\r\nHost: x\r\n\r\n"))
		if err != nil {
			t.Fatalf("target %q: unexpected error: %v", c.target, err)
		}
		if req.Path != c.wantPath {
			t.Errorf("target %q: Path = %q, want %q", c.target, req.Path, c.wantPath)
		}
	}
}

// Query parameter extraction, including multiple parameters.
func TestParseRequest_QueryParameterExtraction(t *testing.T) {
	req, err := httpmsg.ParseRequest([]byte("GET /add?a=2&b=3&op=noop HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := req.Query.Get("a"); got != "2" {
		t.Errorf("a = %q, want 2", got)
	}
	if got := req.Query.Get("b"); got != "3" {
		t.Errorf("b = %q, want 3", got)
	}
	if got := req.Query.Get("op"); got != "noop" {
		t.Errorf("op = %q, want noop", got)
	}
	if req.RawQuery != "a=2&b=3&op=noop" {
		t.Errorf("RawQuery = %q, want a=2&b=3&op=noop", req.RawQuery)
	}
}

// Empty and missing query values.
func TestParseRequest_EmptyAndMissingQueryValues(t *testing.T) {
	req, err := httpmsg.ParseRequest([]byte("GET /add?a=&b&c=3 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := req.Query["a"]; !present || req.Query.Get("a") != "" {
		t.Errorf("a: present=%v, value=%q, want present=true, value=\"\"", present, req.Query.Get("a"))
	}
	if _, present := req.Query["b"]; !present || req.Query.Get("b") != "" {
		t.Errorf("b (no '='): present=%v, value=%q, want present=true, value=\"\"", present, req.Query.Get("b"))
	}
	if req.Query.Get("c") != "3" {
		t.Errorf("c = %q, want 3", req.Query.Get("c"))
	}
	if _, present := req.Query["d"]; present {
		t.Errorf("d was never sent but reported present")
	}

	noQuery, err := httpmsg.ParseRequest([]byte("GET /add HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(noQuery.Query) != 0 {
		t.Errorf("expected no query parameters, got %v", noQuery.Query)
	}
	if noQuery.RawQuery != "" {
		t.Errorf("RawQuery = %q, want empty", noQuery.RawQuery)
	}
}

// Duplicate query parameters keep every value, in order.
func TestParseRequest_DuplicateQueryParameters(t *testing.T) {
	req, err := httpmsg.ParseRequest([]byte("GET /add?a=1&a=2&a=3 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := req.Query["a"]
	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("a = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("a[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Get() on a multi-valued key returns only the first.
	if got := req.Query.Get("a"); got != "1" {
		t.Errorf("Query.Get(a) = %q, want 1 (first value)", got)
	}
}

// Duplicate headers keep every occurrence, in order.
func TestParseRequest_DuplicateHeaders(t *testing.T) {
	req, err := httpmsg.ParseRequest([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: first\r\nHost: second\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	values := req.Headers.Values("Host")
	if len(values) != 2 || values[0] != "first" || values[1] != "second" {
		t.Fatalf("Host values = %v, want [first second]", values)
	}
	if got, _ := req.Headers.Get("Host"); got != "first" {
		t.Errorf("Headers.Get(Host) = %q, want first (first occurrence)", got)
	}
}

// Malformed request lines.
func TestParseRequest_MalformedRequestLines(t *testing.T) {
	cases := map[string]string{
		"missing_version":      "GET /add?a=1&b=2\r\nHost: x\r\n\r\n",
		"missing_target":       "GET HTTP/1.1\r\nHost: x\r\n\r\n",
		"empty_line":           "\r\nHost: x\r\n\r\n",
		"extra_field":          "GET /add HTTP/1.1 extra\r\nHost: x\r\n\r\n",
		"bad_version_syntax":   "GET /add HTTP/1.1beta\r\nHost: x\r\n\r\n",
		"lowercase_http_name":  "GET /add http/1.1\r\nHost: x\r\n\r\n",
		"target_not_origin":    "GET http://example.com/add HTTP/1.1\r\nHost: x\r\n\r\n",
		"target_missing_slash": "GET add HTTP/1.1\r\nHost: x\r\n\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := httpmsg.ParseRequest([]byte(raw))
			if !errors.Is(err, httpmsg.ErrMalformedRequest) {
				t.Fatalf("expected ErrMalformedRequest, got %v", err)
			}
		})
	}
}

// Malformed headers.
func TestParseRequest_MalformedHeaders(t *testing.T) {
	cases := map[string]string{
		"missing_colon":           "GET /add HTTP/1.1\r\nHost example\r\n\r\n",
		"empty_name":              "GET /add HTTP/1.1\r\n: example\r\n\r\n",
		"whitespace_before_colon": "GET /add HTTP/1.1\r\nHost : example\r\n\r\n",
		"leading_space_in_name":   "GET /add HTTP/1.1\r\n Host: example\r\n\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := httpmsg.ParseRequest([]byte(raw))
			if !errors.Is(err, httpmsg.ErrMalformedRequest) {
				t.Fatalf("expected ErrMalformedRequest, got %v", err)
			}
		})
	}
}

// CRLF handling: a bare '\n' left over after splitting on "\r\n" (i.e.
// a line ending that was not a full CRLF pair) is malformed, not tolerated.
func TestParseRequest_BareLineFeedIsMalformed(t *testing.T) {
	// "GET /add HTTP/1.1\nHost: x" has no "\r\n" between the request line
	// and the header, so splitting on "\r\n" leaves them fused into one
	// "line" containing a bare '\n'.
	raw := "GET /add HTTP/1.1\nHost: x\r\n\r\n"
	_, err := httpmsg.ParseRequest([]byte(raw))
	if !errors.Is(err, httpmsg.ErrMalformedRequest) {
		t.Fatalf("expected ErrMalformedRequest, got %v", err)
	}
}

// Header termination behavior: the block must end with a full blank
// line ("\r\n\r\n"); anything else is rejected defensively even though
// reqframe.Framer never hands ParseRequest such input in practice.
func TestParseRequest_RequiresTerminatingBlankLine(t *testing.T) {
	cases := map[string]string{
		"no_terminator_at_all": "GET /add HTTP/1.1\r\nHost: x",
		"single_crlf_only":     "GET /add HTTP/1.1\r\nHost: x\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := httpmsg.ParseRequest([]byte(raw))
			if !errors.Is(err, httpmsg.ErrMalformedRequest) {
				t.Fatalf("expected ErrMalformedRequest, got %v", err)
			}
		})
	}
}

// HTTP version handling.
func TestParseRequest_HTTPVersionHandling(t *testing.T) {
	valid := []string{"HTTP/1.0", "HTTP/1.1", "HTTP/2.0"}
	for _, v := range valid {
		t.Run("valid_"+v, func(t *testing.T) {
			req, err := httpmsg.ParseRequest([]byte("GET /add " + v + "\r\nHost: x\r\n\r\n"))
			if err != nil {
				t.Fatalf("version %q: unexpected error: %v", v, err)
			}
			if req.Version != v {
				t.Errorf("Version = %q, want %q", req.Version, v)
			}
		})
	}

	invalid := []string{"HTTP/1", "HTTP/", "HTTP1.1", "http/1.1", "HTTP/1.1.1", "FOO/1.1"}
	for _, v := range invalid {
		t.Run("invalid_"+v, func(t *testing.T) {
			_, err := httpmsg.ParseRequest([]byte("GET /add " + v + "\r\nHost: x\r\n\r\n"))
			if !errors.Is(err, httpmsg.ErrMalformedRequest) {
				t.Fatalf("version %q: expected ErrMalformedRequest, got %v", v, err)
			}
		})
	}
}

// Representative requests from the assignment itself.
func TestParseRequest_RepresentativeAssignmentRequests(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantMethod string
		wantPath   string
		wantQuery  map[string]string
	}{
		{"add", "GET /add?a=2&b=3 HTTP/1.1\r\nHost: localhost\r\n\r\n", "GET", "/add", map[string]string{"a": "2", "b": "3"}},
		{"sub", "GET /sub?a=10&b=4 HTTP/1.1\r\nHost: localhost\r\n\r\n", "GET", "/sub", map[string]string{"a": "10", "b": "4"}},
		{"mul", "GET /mul?a=6&b=7 HTTP/1.1\r\nHost: localhost\r\n\r\n", "GET", "/mul", map[string]string{"a": "6", "b": "7"}},
		{"div", "GET /div?a=9&b=3 HTTP/1.1\r\nHost: localhost\r\n\r\n", "GET", "/div", map[string]string{"a": "9", "b": "3"}},
		{"pow_unsupported_but_parseable", "GET /pow?a=2&b=8 HTTP/1.1\r\nHost: localhost\r\n\r\n", "GET", "/pow", map[string]string{"a": "2", "b": "8"}},
		{"post_add", "POST /add HTTP/1.1\r\nHost: localhost\r\n\r\n", "POST", "/add", map[string]string{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := httpmsg.ParseRequest([]byte(c.raw))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.Method != c.wantMethod {
				t.Errorf("Method = %q, want %q", req.Method, c.wantMethod)
			}
			if req.Path != c.wantPath {
				t.Errorf("Path = %q, want %q", req.Path, c.wantPath)
			}
			for k, want := range c.wantQuery {
				if got := req.Query.Get(k); got != want {
					t.Errorf("Query[%q] = %q, want %q", k, got, want)
				}
			}
		})
	}
}

// Percent-encoding decisions: '+' in the raw (unencoded) query means space,
// per application/x-www-form-urlencoded convention; %2B is a literal '+'.
// Invalid percent-encoding anywhere in the target is a parse error with no
// partial recovery.
func TestParseRequest_PercentEncodingDecisions(t *testing.T) {
	plus, err := httpmsg.ParseRequest([]byte("GET /add?a=1+2 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := plus.Query.Get("a"); got != "1 2" {
		t.Errorf("literal '+' decoded to %q, want \"1 2\"", got)
	}

	encodedPlus, err := httpmsg.ParseRequest([]byte("GET /add?a=1%2B2 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := encodedPlus.Query.Get("a"); got != "1+2" {
		t.Errorf("%%2B decoded to %q, want \"1+2\"", got)
	}

	_, err = httpmsg.ParseRequest([]byte("GET /add?a=%zz HTTP/1.1\r\nHost: x\r\n\r\n"))
	if !errors.Is(err, httpmsg.ErrMalformedRequest) {
		t.Fatalf("invalid query percent-encoding: expected ErrMalformedRequest, got %v", err)
	}

	_, err = httpmsg.ParseRequest([]byte("GET /%zz HTTP/1.1\r\nHost: x\r\n\r\n"))
	if !errors.Is(err, httpmsg.ErrMalformedRequest) {
		t.Fatalf("invalid path percent-encoding: expected ErrMalformedRequest, got %v", err)
	}
}

// Unsupported operations (routing's concern) and invalid numeric values
// (validation's concern) must both still parse successfully.
func TestParseRequest_DoesNotRejectRoutingOrValidationConcerns(t *testing.T) {
	unsupportedOp, err := httpmsg.ParseRequest([]byte("GET /pow?a=2&b=8 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("unsupported operation should still parse: %v", err)
	}
	if unsupportedOp.Path != "/pow" {
		t.Errorf("Path = %q, want /pow", unsupportedOp.Path)
	}

	nonNumeric, err := httpmsg.ParseRequest([]byte("GET /add?a=x&b=3 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if err != nil {
		t.Fatalf("non-numeric parameter should still parse: %v", err)
	}
	if got := nonNumeric.Query.Get("a"); got != "x" {
		t.Errorf("a = %q, want x (unvalidated)", got)
	}
}
