package httpmsg

// Status is one of the four HTTP status codes this server ever returns.
type Status int

const (
	StatusOK               Status = 200
	StatusBadRequest       Status = 400
	StatusNotFound         Status = 404
	StatusMethodNotAllowed Status = 405
)

// ReasonPhrase is the one source of truth for each status's reason text -
// used in the status line, and, for non-2xx responses, as the body itself
// (a short reason string, e.g. "Bad Request").
func (s Status) ReasonPhrase() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusBadRequest:
		return "Bad Request"
	case StatusNotFound:
		return "Not Found"
	case StatusMethodNotAllowed:
		return "Method Not Allowed"
	default:
		return ""
	}
}
