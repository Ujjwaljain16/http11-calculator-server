package httpmsg

// Status is an HTTP status code. Only the codes this server sends are defined.
type Status int

const (
	StatusOK               Status = 200
	StatusBadRequest       Status = 400
	StatusNotFound         Status = 404
	StatusMethodNotAllowed Status = 405
)

// ReasonPhrase returns the standard reason phrase for s. It is used in the
// status line and as the body of error responses.
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
