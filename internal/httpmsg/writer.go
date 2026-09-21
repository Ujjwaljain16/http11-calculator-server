package httpmsg

import (
	"bytes"
	"io"
	"strconv"
)

// WriteResponse writes resp to w as a status line, headers, a blank line and
// the body, with every line ending in CRLF. The whole response is written
// with a single call to w.Write.
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
