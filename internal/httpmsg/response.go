package httpmsg

import "strconv"

// Response is a response's contents, independent of sockets or wire bytes.
// WriteResponse (writer.go) is what turns it into bytes.
type Response struct {
	Version string
	Status  Status
	Headers Headers
	Body    []byte
}

// NewResponse builds a Response with the headers this server always sends:
// Content-Type, Content-Length (computed from body's actual byte length,
// not an assumed character count), and Connection: keep-alive. version is
// normally the request's own HTTP-version token, echoed back.
func NewResponse(version string, status Status, body string) Response {
	return newResponse(version, status, body, "keep-alive")
}

// NewCloseResponse is NewResponse but with Connection: close instead of
// keep-alive - for the last response on a connection that is about to be
// closed, whether because the client asked for that (its own request had
// a Connection: close header) or because the server itself decided the
// connection can't continue.
func NewCloseResponse(version string, status Status, body string) Response {
	return newResponse(version, status, body, "close")
}

func newResponse(version string, status Status, body, connection string) Response {
	bodyBytes := []byte(body)
	return Response{
		Version: version,
		Status:  status,
		Headers: Headers{
			{Name: "Content-Type", Value: "text/plain"},
			{Name: "Content-Length", Value: strconv.Itoa(len(bodyBytes))},
			{Name: "Connection", Value: connection},
		},
		Body: bodyBytes,
	}
}
