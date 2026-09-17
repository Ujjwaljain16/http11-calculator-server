package httpmsg

import (
	"bytes"
	"io"
	"strconv"
)

// WriteResponse serializes resp to w: status line, headers, a blank line,
// then the body - CRLF everywhere. It only serializes what resp already
// contains; it never opens a socket, and never decides what status or body
// a request deserves.
func WriteResponse(w io.Writer, resp Response) error {
	var buf bytes.Buffer

	buf.WriteString(resp.Version)
	buf.WriteByte(' ')
	buf.WriteString(strconv.Itoa(int(resp.Status)))
	buf.WriteByte(' ')
	buf.WriteString(resp.Status.ReasonPhrase())
	buf.WriteString("\r\n")

	for _, h := range resp.Headers {
		buf.WriteString(h.Name)
		buf.WriteString(": ")
		buf.WriteString(h.Value)
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	buf.Write(resp.Body)

	_, err := w.Write(buf.Bytes())
	return err
}
