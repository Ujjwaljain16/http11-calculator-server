// Package validate checks the Host header and the "a" and "b" query
// parameters of a parsed request. It does not look at the method or path.
package validate

import (
	"strconv"

	"calcserver/internal/httpmsg"
)

// A Param is the outcome of validating one query parameter. Value is
// meaningful only when OK is true.
type Param struct {
	OK    bool
	Value int64
}

// A Result is the outcome of validating a request.
type Result struct {
	HostOK bool
	A      Param
	B      Param
}

// OK reports whether the request passed every check.
func (r Result) OK() bool {
	return r.HostOK && r.A.OK && r.B.OK
}

// Validate checks that req has a Host header (any value, including empty)
// and that its "a" and "b" query parameters are integers. If a parameter is
// repeated, the first value is used.
func Validate(req httpmsg.Request) Result {
	_, hostOK := req.Headers.Get("Host")
	return Result{
		HostOK: hostOK,
		A:      validateParam(req.Query.Get("a")),
		B:      validateParam(req.Query.Get("b")),
	}
}

// validateParam accepts only a non-empty base-10 integer that fits in an
// int64. A missing parameter arrives here as an empty string and is rejected
// with the other invalid values.
func validateParam(raw string) Param {
	if raw == "" {
		return Param{}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return Param{}
	}
	return Param{OK: true, Value: n}
}
