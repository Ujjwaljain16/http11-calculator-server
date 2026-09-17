package router

import (
	"strconv"

	"calcserver/internal/httpmsg"
)

// statusFor is the one place an Outcome becomes an HTTP status - Outcome
// stays domain-level everywhere else in this package.
var statusFor = map[Outcome]httpmsg.Status{
	OutcomeOK:               httpmsg.StatusOK,
	OutcomeNotFound:         httpmsg.StatusNotFound,
	OutcomeMethodNotAllowed: httpmsg.StatusMethodNotAllowed,
	OutcomeBadRequest:       httpmsg.StatusBadRequest,
}

// Respond turns an already-determined Decision into its HTTP response. It
// makes no decisions of its own beyond Outcome->status and close->
// Connection header: version is normally the request's own HTTP-version
// token, echoed back; close is whether the caller has already decided
// this is the last response on the connection (e.g. the client's own
// request carried a Connection: close header) - Respond does not decide
// that itself, only reflects it in the emitted Connection header. A
// successful calculation's body is its numeric result; every other
// outcome's body is simply its status's reason phrase.
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
