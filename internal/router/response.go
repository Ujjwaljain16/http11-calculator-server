package router

import (
	"strconv"

	"calcserver/internal/httpmsg"
)

// statusFor is the one place an Outcome becomes an HTTP status - Phase 5's
// outcomes stay domain-level everywhere else.
var statusFor = map[Outcome]httpmsg.Status{
	OutcomeOK:               httpmsg.StatusOK,
	OutcomeNotFound:         httpmsg.StatusNotFound,
	OutcomeMethodNotAllowed: httpmsg.StatusMethodNotAllowed,
	OutcomeBadRequest:       httpmsg.StatusBadRequest,
}

// Respond turns an already-determined Decision into its HTTP response. It
// makes no decisions of its own: version is normally the request's own
// HTTP-version token, echoed back. A successful calculation's body is its
// numeric result; every other outcome's body is simply its status's
// reason phrase (Part 8's documented short-reason-string bodies).
func Respond(d Decision, version string) httpmsg.Response {
	status := statusFor[d.Outcome]

	body := status.ReasonPhrase()
	if status == httpmsg.StatusOK {
		body = strconv.FormatInt(d.Result, 10)
	}

	return httpmsg.NewResponse(version, status, body)
}
