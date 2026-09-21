package httpmsg

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrMalformedRequest is wrapped by every error ParseRequest returns for
// syntactically invalid input. Semantic problems such as an unknown path, a
// non-numeric parameter or a missing Host header are not parse errors.
var ErrMalformedRequest = errors.New("malformed request")

// ParseRequest parses one request as returned by reqframe.Framer.Next: a
// request line, header lines, and a terminating blank line, all ending in
// CRLF.
//
// Parsing rules:
//
//   - The request line has exactly three fields separated by single spaces:
//     method, target and version. Any method is accepted; the router decides
//     whether it is supported.
//   - The version must have the form HTTP/<digits>.<digits>. The number
//     itself is not checked.
//   - The target must start with '/'. Its path is percent-decoded with
//     url.PathUnescape and its query string parsed with url.ParseQuery, so
//     '+' in a query decodes to a space. Invalid percent-encoding makes the
//     request malformed.
//   - Query parameters and headers keep every occurrence, in order.
//     Empty values ("a=") and bare names ("a") both give an empty value.
//   - A header line needs a colon, a non-empty name, and no whitespace
//     between the name and the colon. Leading and trailing whitespace is
//     trimmed from the value, which may be empty.
//   - A bare CR or LF inside the request line or a header line makes the
//     request malformed.
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

	path, query, err := parseTarget(target)
	if err != nil {
		return Request{}, err
	}

	return Request{
		Method:  method,
		Path:    path,
		Query:   query,
		Version: version,
		Headers: headers,
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

func parseTarget(target string) (path string, query url.Values, err error) {
	if !strings.HasPrefix(target, "/") {
		return "", nil, fmt.Errorf("%w: request-target must be in origin-form, starting with '/': %q", ErrMalformedRequest, target)
	}

	rawPath, rawQuery, _ := strings.Cut(target, "?")

	path, decodeErr := url.PathUnescape(rawPath)
	if decodeErr != nil {
		return "", nil, fmt.Errorf("%w: invalid percent-encoding in path: %v", ErrMalformedRequest, decodeErr)
	}

	query, queryErr := url.ParseQuery(rawQuery)
	if queryErr != nil {
		return "", nil, fmt.Errorf("%w: invalid query string: %v", ErrMalformedRequest, queryErr)
	}

	return path, query, nil
}
