// Package router turns a parsed Request plus its validate.Result into an
// in-memory routing Decision: known-path/GET/valid-params requests get
// computed by calc; everything else gets one of the three failure
// outcomes. Router never produces HTTP status lines or response bytes -
// that translation happens in response.go.
package router

import (
	"calcserver/internal/calc"
	"calcserver/internal/httpmsg"
	"calcserver/internal/validate"
)

// Outcome names a routing decision without naming any HTTP status code.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeNotFound
	OutcomeMethodNotAllowed
	OutcomeBadRequest
)

// Decision is the router's result. Result is meaningful only when
// Outcome is OutcomeOK.
type Decision struct {
	Outcome Outcome
	Result  int64
}

// operations maps each supported path to its calculator function. An
// unlisted path (e.g. "/pow") is exactly what makes a request unknown.
var operations = map[string]func(a, b int64) (int64, error){
	"/add": calc.Add,
	"/sub": calc.Sub,
	"/mul": calc.Mul,
	"/div": calc.Div,
}

// Route decides what to do with req, given its already-computed validation
// result v. It checks, in order: is the path one of the four supported
// operations (else NotFound); is the method GET (else MethodNotAllowed);
// did Host/"a"/"b" validate (else BadRequest); did the arithmetic itself
// succeed - no division by zero, no overflow (else BadRequest).
func Route(req httpmsg.Request, v validate.Result) Decision {
	op, known := operations[req.Path]
	if !known {
		return Decision{Outcome: OutcomeNotFound}
	}
	if req.Method != "GET" {
		return Decision{Outcome: OutcomeMethodNotAllowed}
	}
	if !v.OK() {
		return Decision{Outcome: OutcomeBadRequest}
	}

	result, err := op(v.A.Value, v.B.Value)
	if err != nil {
		return Decision{Outcome: OutcomeBadRequest}
	}
	return Decision{Outcome: OutcomeOK, Result: result}
}
