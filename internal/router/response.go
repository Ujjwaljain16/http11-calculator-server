package router

import (
	"strconv"

	"calcserver/internal/httpmsg"
)

// statusFor maps each Outcome to its HTTP status.
var statusFor = map[Outcome]httpmsg.Status{
	OutcomeOK:               httpmsg.StatusOK,
	OutcomeNotFound:         httpmsg.StatusNotFound,
	OutcomeMethodNotAllowed: httpmsg.StatusMethodNotAllowed,
	OutcomeBadRequest:       httpmsg.StatusBadRequest,
}

// Respond builds the HTTP response for d. version is the HTTP version to
// use in the status line. If close is true the response carries
// "Connection: close", otherwise "Connection: keep-alive". The body is the
// result for a successful calculation and the reason phrase otherwise.
func Respond(d Decision, version string, close bool) httpmsg.Response {
	status := statusFor[d.Outcome]

	body := status.ReasonPhrase()
	if status == httpmsg.StatusOK {
		body = strconv.FormatInt(d.Result, 10)
	}

	if close {
		return httpmsg.NewCloseResponse(version, status, body)
	}
	return httpmsg.NewResponse(version, status, body)
}
