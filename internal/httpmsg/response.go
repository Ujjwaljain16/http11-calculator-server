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
	bodyBytes := []byte(body)
	return Response{
		Version: version,
		Status:  status,
		Headers: Headers{
			{Name: "Content-Type", Value: "text/plain"},
			{Name: "Content-Length", Value: strconv.Itoa(len(bodyBytes))},
			{Name: "Connection", Value: "keep-alive"},
		},
		Body: bodyBytes,
	}
}
