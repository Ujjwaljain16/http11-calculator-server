// Package router decides how to answer a parsed request: it selects the
// arithmetic operation for the path and produces a Decision. Turning a
// Decision into an HTTP response is done by Respond.
package router

import (
	"calcserver/internal/calc"
	"calcserver/internal/httpmsg"
	"calcserver/internal/validate"
)

// An Outcome is the kind of answer a request should receive.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeNotFound
	OutcomeMethodNotAllowed
	OutcomeBadRequest
)

// A Decision is the result of routing a request. Result is meaningful only
// when Outcome is OutcomeOK.
type Decision struct {
	Outcome Outcome
	Result  int64
}

// operations maps each supported path to its arithmetic function.
var operations = map[string]func(a, b int64) (int64, error){
	"/add": calc.Add,
	"/sub": calc.Sub,
	"/mul": calc.Mul,
	"/div": calc.Div,
}

// Route decides the outcome for req, where v is the result of validating it.
// The checks run in this order: unknown path gives OutcomeNotFound, a method
// other than GET gives OutcomeMethodNotAllowed, failed validation gives
// OutcomeBadRequest, and an arithmetic error (overflow or division by zero)
// also gives OutcomeBadRequest. Otherwise the outcome is OutcomeOK with the
// computed result.
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
