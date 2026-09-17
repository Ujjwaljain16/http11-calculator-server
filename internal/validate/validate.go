// Package validate checks a parsed Request's Host header and "a"/"b" query
// parameters. It never looks at Method or Path and never decides whether an
// operation or method is supported - that is routing's job in a later
// phase. This package only answers: if something downstream treats this as
// a calculator request, are its Host and parameters usable?
package validate

import (
	"strconv"

	"calcserver/internal/httpmsg"
)

// Param is one query parameter's validation outcome. Value is meaningful
// only when OK is true.
type Param struct {
	OK    bool
	Value int64
}

// Result is a Request's validation outcome. It carries no HTTP status;
// callers decide what to do with a failed field.
type Result struct {
	HostOK bool
	A      Param
	B      Param
}

// OK reports whether every checked field passed.
func (r Result) OK() bool {
	return r.HostOK && r.A.OK && r.B.OK
}

// Validate checks req.Headers for a Host header (presence only - an empty
// value still counts as present, since Part 8 only lists a *missing* Host
// as a failure trigger) and validates "a"/"b" as described by validateParam.
func Validate(req httpmsg.Request) Result {
	_, hostOK := req.Headers.Get("Host")
	return Result{
		HostOK: hostOK,
		A:      validateParam(req.Query.Get("a")),
		B:      validateParam(req.Query.Get("b")),
	}
}

// validateParam accepts a value only if it parses in its entirety as a
// base-10 signed 64-bit integer.
//
// Query.Get returns "" both when a parameter was never sent and when it was
// sent empty (Phase 3's documented behavior for "a=" and bare "a"); this
// subset treats both the same way, so no separate check is needed. For
// duplicates, Query.Get already returns the first value (also a Phase 3
// decision), so that policy is inherited automatically rather than
// re-implemented here.
//
// strconv.ParseInt rejects non-numeric text, decimals, and any value
// outside the int64 range (parsing overflow) uniformly as one syntax
// error - the spec draws no distinction between these failure shapes, only
// between "usable" and "not usable". Arithmetic overflow (e.g. a+b
// overflowing int64) is a different concern and belongs to calculator
// execution in Phase 5, not here.
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
