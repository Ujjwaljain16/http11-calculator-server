package httpmsg

import "strconv"

// A Response is an HTTP response. WriteResponse turns it into bytes.
type Response struct {
	Version string
	Status  Status
	Headers Headers
	Body    []byte
}

// NewResponse returns a plain-text response with Content-Length set to the
// byte length of body and "Connection: keep-alive".
func NewResponse(version string, status Status, body string) Response {
	return newResponse(version, status, body, "keep-alive")
}

// NewCloseResponse is like NewResponse but sets "Connection: close", for the
// last response on a connection.
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
