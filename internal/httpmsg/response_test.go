package httpmsg_test

import (
	"strconv"
	"testing"

	"calcserver/internal/httpmsg"
)

// Content-Length must equal the response body's exact byte count, computed
// from the body's own bytes - never an assumed character count.
func TestNewResponse_ContentLengthMatchesActualBodyBytes(t *testing.T) {
	cases := map[string]string{
		"empty_body":      "",
		"one_byte_body":   "5",
		"multi_byte_body": "Method Not Allowed",
		"numeric_body":    "9223372036854775807",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := httpmsg.NewResponse("HTTP/1.1", httpmsg.StatusOK, body)

			gotLen, ok := resp.Headers.Get("Content-Length")
			if !ok {
				t.Fatalf("Content-Length header missing")
			}
			if want := strconv.Itoa(len(resp.Body)); gotLen != want {
				t.Errorf("Content-Length = %q, want %q", gotLen, want)
			}
			if len(resp.Body) != len([]byte(body)) {
				t.Errorf("Body byte length = %d, want %d", len(resp.Body), len([]byte(body)))
			}
		})
	}
}

func TestNewResponse_SetsRequiredHeaders(t *testing.T) {
	resp := httpmsg.NewResponse("HTTP/1.1", httpmsg.StatusOK, "5")

	if ct, ok := resp.Headers.Get("Content-Type"); !ok || ct != "text/plain" {
		t.Errorf("Content-Type = %q, ok=%v, want text/plain, true", ct, ok)
	}
	if conn, ok := resp.Headers.Get("Connection"); !ok || conn != "keep-alive" {
		t.Errorf("Connection = %q, ok=%v, want keep-alive, true", conn, ok)
	}
	if _, ok := resp.Headers.Get("Content-Length"); !ok {
		t.Errorf("Content-Length header missing")
	}
}
