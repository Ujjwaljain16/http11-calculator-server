// Package httpmsg parses HTTP-style requests and builds and writes
// responses.
package httpmsg

import (
	"net/url"
	"strings"
)

// A Request is a parsed request. It records what the client sent and has not
// been checked against any application rules.
type Request struct {
	Method string
	// Path is the request-target's path, percent-decoded.
	Path string
	// Query holds the request-target's query parameters, percent-decoded.
	// A repeated parameter keeps all its values in order.
	Query   url.Values
	Version string
	Headers Headers
}

// A Header is one header field. Name keeps the case the client used; Value
// has surrounding whitespace removed.
type Header struct {
	Name  string
	Value string
}

// Headers holds header fields in the order received, including repeats.
type Headers []Header

// Get returns the value of the first header called name, ignoring case,
// and whether one was present.
func (h Headers) Get(name string) (string, bool) {
	for _, hd := range h {
		if strings.EqualFold(hd.Name, name) {
			return hd.Value, true
		}
	}
	return "", false
}
