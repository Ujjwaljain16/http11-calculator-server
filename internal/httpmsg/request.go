// Package httpmsg holds the structured request representation for this
// server's HTTP subset, and the parser that turns one framed request block
// (as produced by internal/reqframe) into that representation.
package httpmsg

import (
	"net/url"
	"strings"
)

// Request is the parsed form of one request. Every field is exactly what
// the client sent, decoded only where the request-target syntax itself
// requires it (percent-encoding). Nothing here has been validated against
// application rules (Host presence, numeric parameters, known
// operations, ...) - that is validate and router's responsibility.
type Request struct {
	Method string
	// Path is the request-target's path component, percent-decoded.
	Path string
	// Query holds the request-target's query string (after the first '?')
	// parsed into percent-decoded key/value pairs. Duplicate keys keep
	// every value, in the order they appeared.
	Query url.Values
	// Version is the request-line's HTTP-version token, e.g. "HTTP/1.1".
	Version string
	Headers Headers
}

// Header is one header field exactly as received: Name keeps its original
// case, Value has only leading/trailing whitespace trimmed.
type Header struct {
	Name  string
	Value string
}

// Headers preserves every header field in the order it was sent, including
// duplicates. Header field names are case-insensitive per HTTP, so lookups
// use case-insensitive comparison; nothing here judges whether a duplicate
// or missing header is valid.
type Headers []Header

// Get returns the value of the first header named name (case-insensitive)
// and true, or "", false if no such header was sent.
func (h Headers) Get(name string) (string, bool) {
	for _, hd := range h {
		if strings.EqualFold(hd.Name, name) {
			return hd.Value, true
		}
	}
	return "", false
}
