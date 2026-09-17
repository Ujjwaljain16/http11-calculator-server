package router_test

import (
	"testing"

	"calcserver/internal/httpmsg"
	"calcserver/internal/router"
)

// Respond only needs to prove its own translation (Outcome -> Status,
// Result -> body text); byte-level serialization is httpmsg's own tested
// concern, not re-verified here.
func TestRespond_MapsOutcomeToStatusAndBody(t *testing.T) {
	cases := []struct {
		name       string
		decision   router.Decision
		wantStatus httpmsg.Status
		wantBody   string
	}{
		{"ok", router.Decision{Outcome: router.OutcomeOK, Result: 42}, httpmsg.StatusOK, "42"},
		{"ok_negative_result", router.Decision{Outcome: router.OutcomeOK, Result: -7}, httpmsg.StatusOK, "-7"},
		{"bad_request", router.Decision{Outcome: router.OutcomeBadRequest}, httpmsg.StatusBadRequest, "Bad Request"},
		{"not_found", router.Decision{Outcome: router.OutcomeNotFound}, httpmsg.StatusNotFound, "Not Found"},
		{"method_not_allowed", router.Decision{Outcome: router.OutcomeMethodNotAllowed}, httpmsg.StatusMethodNotAllowed, "Method Not Allowed"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := router.Respond(c.decision, "HTTP/1.1", false)
			if resp.Status != c.wantStatus {
				t.Errorf("Status = %d, want %d", resp.Status, c.wantStatus)
			}
			if string(resp.Body) != c.wantBody {
				t.Errorf("Body = %q, want %q", resp.Body, c.wantBody)
			}
			if resp.Version != "HTTP/1.1" {
				t.Errorf("Version = %q, want HTTP/1.1", resp.Version)
			}
			if got, _ := resp.Headers.Get("Connection"); got != "keep-alive" {
				t.Errorf("Connection = %q, want keep-alive", got)
			}
		})
	}
}

// close=true must be reflected in the Connection header regardless of
// which outcome produced the response - the client asked to close after
// this response, independent of its status.
func TestRespond_CloseReflectsInConnectionHeader(t *testing.T) {
	cases := []struct {
		name     string
		decision router.Decision
	}{
		{"ok", router.Decision{Outcome: router.OutcomeOK, Result: 5}},
		{"bad_request", router.Decision{Outcome: router.OutcomeBadRequest}},
		{"not_found", router.Decision{Outcome: router.OutcomeNotFound}},
		{"method_not_allowed", router.Decision{Outcome: router.OutcomeMethodNotAllowed}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := router.Respond(c.decision, "HTTP/1.1", true)
			if got, _ := resp.Headers.Get("Connection"); got != "close" {
				t.Errorf("Connection = %q, want close", got)
			}
		})
	}
}
