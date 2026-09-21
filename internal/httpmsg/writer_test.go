package httpmsg_test

import (
	"bytes"
	"testing"

	"calcserver/internal/httpmsg"
)

// TestWriteResponse_ByteExact compares the complete bytes written for each
// status against the expected text, with "\r\n" spelled out.
func TestWriteResponse_ByteExact(t *testing.T) {
	cases := []struct {
		name   string
		status httpmsg.Status
		body   string
		want   string
	}{
		{
			name:   "200_OK",
			status: httpmsg.StatusOK,
			body:   "5",
			want: "HTTP/1.1 200 OK\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Length: 1\r\n" +
				"Connection: keep-alive\r\n" +
				"\r\n" +
				"5",
		},
		{
			name:   "400_BadRequest",
			status: httpmsg.StatusBadRequest,
			body:   "Bad Request",
			want: "HTTP/1.1 400 Bad Request\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Length: 11\r\n" +
				"Connection: keep-alive\r\n" +
				"\r\n" +
				"Bad Request",
		},
		{
			name:   "404_NotFound",
			status: httpmsg.StatusNotFound,
			body:   "Not Found",
			want: "HTTP/1.1 404 Not Found\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Length: 9\r\n" +
				"Connection: keep-alive\r\n" +
				"\r\n" +
				"Not Found",
		},
		{
			name:   "405_MethodNotAllowed",
			status: httpmsg.StatusMethodNotAllowed,
			body:   "Method Not Allowed",
			want: "HTTP/1.1 405 Method Not Allowed\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Length: 18\r\n" +
				"Connection: keep-alive\r\n" +
				"\r\n" +
				"Method Not Allowed",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := httpmsg.NewResponse("HTTP/1.1", c.status, c.body)

			var buf bytes.Buffer
			if err := httpmsg.WriteResponse(&buf, resp); err != nil {
				t.Fatalf("WriteResponse: %v", err)
			}

			got := buf.Bytes()
			want := []byte(c.want)
			if !bytes.Equal(got, want) {
				t.Fatalf("serialized bytes mismatch\n got: %q\nwant: %q", got, want)
			}

			assertNoLoneLineFeed(t, got)
			assertEndsWithBlankLineThenBody(t, got, c.body)
		})
	}
}

// assertNoLoneLineFeed fails if data contains a '\n' not preceded by '\r'.
func assertNoLoneLineFeed(t *testing.T, data []byte) {
	t.Helper()
	for i, b := range data {
		if b == '\n' && (i == 0 || data[i-1] != '\r') {
			t.Fatalf("found a bare '\\n' at byte %d not preceded by '\\r': %q", i, data)
		}
	}
}

// assertEndsWithBlankLineThenBody checks that the bytes after the first blank
// line are exactly body.
func assertEndsWithBlankLineThenBody(t *testing.T, data []byte, body string) {
	t.Helper()
	idx := bytes.Index(data, []byte("\r\n\r\n"))
	if idx < 0 {
		t.Fatalf("no blank line (\\r\\n\\r\\n) found in response: %q", data)
	}
	gotBody := data[idx+len("\r\n\r\n"):]
	if !bytes.Equal(gotBody, []byte(body)) {
		t.Fatalf("bytes after blank line = %q, want body %q", gotBody, body)
	}
}

func TestWriteResponse_EmptyBody(t *testing.T) {
	resp := httpmsg.NewResponse("HTTP/1.1", httpmsg.StatusOK, "")

	var buf bytes.Buffer
	if err := httpmsg.WriteResponse(&buf, resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}

	want := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/plain\r\n" +
		"Content-Length: 0\r\n" +
		"Connection: keep-alive\r\n" +
		"\r\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
