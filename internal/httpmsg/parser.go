package httpmsg

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrMalformedRequest wraps every syntax-level parse failure. Callers can
// check for it with errors.Is; the wrapped message gives the specific
// reason. Parsing never reports on application-level concerns (unknown
// operation, invalid number, missing Host, ...) - only on request syntax
// that this subset's grammar (plan.md Part 4) cannot represent at all.
var ErrMalformedRequest = errors.New("malformed request")

// ParseRequest turns one framed request block - the exact bytes returned by
// reqframe.Framer.Next(), i.e. ending in "\r\n\r\n" - into a Request.
//
// Documented parsing decisions (see assignment/plan.md Part 4 for the
// grammar these implement):
//
//   - Duplicate headers: every occurrence is kept, in order (Headers is a
//     slice, not a map). Whether a duplicate is acceptable is a later
//     phase's decision.
//   - Duplicate query parameters: every value is kept, in order
//     (Query is net/url.Values, i.e. map[string][]string).
//   - Empty query values ("a=") and bare parameter names without '='
//     ("a") both produce an entry with value "" - both mean "present,
//     empty", per net/url.ParseQuery's own behavior.
//   - Percent-encoding: the path is decoded with url.PathUnescape; the
//     query string is decoded with url.ParseQuery, which follows
//     application/x-www-form-urlencoded rules (a literal '+' decodes to
//     space; use %2B for a literal plus). Invalid percent-encoding
//     anywhere in the target makes the whole request malformed - there is
//     no partial recovery.
//   - Malformed request lines: the line must be exactly three fields
//     separated by a single space (METHOD, TARGET, VERSION); any other
//     count of fields is malformed. The method is not restricted to known
//     verbs here - an unsupported method (e.g. POST) still parses; only
//     routing (a later phase) rejects it.
//   - The request-target must be in origin-form, starting with '/' (per
//     Part 4: no absolute-URI, "*", or authority form is supported).
//   - Malformed header lines: a header line must contain a colon, with a
//     non-empty name and no whitespace between the name and the colon
//     (whitespace there is a known request-smuggling ambiguity, so it is
//     rejected rather than tolerated). The value has leading/trailing
//     whitespace trimmed but may be empty.
//   - CRLF handling: request-line and header-line text must not contain a
//     bare '\r' or '\n' left over after splitting on "\r\n" - such a
//     leftover means the client used an inconsistent line ending, which
//     is treated as malformed rather than silently accepted.
//   - HTTP version: only the syntactic shape "HTTP/<digits>.<digits>" is
//     checked (case-sensitive "HTTP/", per RFC 7230's fixed HTTP-name).
//     The specific version number is not rejected or special-cased - this
//     subset defines no version-dependent behavior.
func ParseRequest(raw []byte) (Request, error) {
	s := string(raw)

	lines := strings.Split(s, "\r\n")
	if len(lines) < 3 || lines[len(lines)-1] != "" || lines[len(lines)-2] != "" {
		return Request{}, fmt.Errorf("%w: request block does not end with a blank line (\\r\\n\\r\\n)", ErrMalformedRequest)
	}
	headerLines := lines[1 : len(lines)-2]

	method, target, version, err := parseRequestLine(lines[0])
	if err != nil {
		return Request{}, err
	}

	headers, err := parseHeaders(headerLines)
	if err != nil {
		return Request{}, err
	}

	path, rawQuery, query, err := parseTarget(target)
	if err != nil {
		return Request{}, err
	}

	return Request{
		Method:   method,
		Target:   target,
		Path:     path,
		RawQuery: rawQuery,
		Query:    query,
		Version:  version,
		Headers:  headers,
	}, nil
}

func parseRequestLine(line string) (method, target, version string, err error) {
	if strings.ContainsAny(line, "\r\n") {
		return "", "", "", fmt.Errorf("%w: request line contains a bare CR or LF", ErrMalformedRequest)
	}

	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("%w: request line must have exactly three space-separated fields, got %q", ErrMalformedRequest, line)
	}
	method, target, version = parts[0], parts[1], parts[2]
	if method == "" || target == "" || version == "" {
		return "", "", "", fmt.Errorf("%w: request line has an empty field: %q", ErrMalformedRequest, line)
	}
	if strings.Contains(version, " ") {
		return "", "", "", fmt.Errorf("%w: too many fields in request line: %q", ErrMalformedRequest, line)
	}
	if !isValidHTTPVersion(version) {
		return "", "", "", fmt.Errorf("%w: unrecognized HTTP-version syntax %q", ErrMalformedRequest, version)
	}

	return method, target, version, nil
}

func isValidHTTPVersion(v string) bool {
	rest, ok := strings.CutPrefix(v, "HTTP/")
	if !ok {
		return false
	}
	major, minor, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	return major != "" && minor != "" && isAllDigits(major) && isAllDigits(minor)
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseHeaders(lines []string) (Headers, error) {
	headers := make(Headers, 0, len(lines))
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return nil, fmt.Errorf("%w: header line contains a bare CR or LF: %q", ErrMalformedRequest, line)
		}

		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%w: header line missing colon: %q", ErrMalformedRequest, line)
		}
		if name == "" {
			return nil, fmt.Errorf("%w: header line has an empty name: %q", ErrMalformedRequest, line)
		}
		if strings.TrimSpace(name) != name {
			return nil, fmt.Errorf("%w: whitespace between header name and colon: %q", ErrMalformedRequest, line)
		}

		headers = append(headers, Header{Name: name, Value: strings.TrimSpace(value)})
	}
	return headers, nil
}

func parseTarget(target string) (path, rawQuery string, query url.Values, err error) {
	if !strings.HasPrefix(target, "/") {
		return "", "", nil, fmt.Errorf("%w: request-target must be in origin-form, starting with '/': %q", ErrMalformedRequest, target)
	}

	rawPath, rawQuery, _ := strings.Cut(target, "?")

	path, decodeErr := url.PathUnescape(rawPath)
	if decodeErr != nil {
		return "", "", nil, fmt.Errorf("%w: invalid percent-encoding in path: %v", ErrMalformedRequest, decodeErr)
	}

	query, queryErr := url.ParseQuery(rawQuery)
	if queryErr != nil {
		return "", "", nil, fmt.Errorf("%w: invalid query string: %v", ErrMalformedRequest, queryErr)
	}

	return path, rawQuery, query, nil
}
