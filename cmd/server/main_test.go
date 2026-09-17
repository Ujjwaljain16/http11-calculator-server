package main

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"calcserver/internal/httpmsg"
	"calcserver/internal/reqframe"
)

// startCalcServer runs the same accept-loop/serveConn logic main() uses,
// on a real ephemeral TCP port, so tests exercise the actual orchestration.
func startCalcServer(t *testing.T) net.Listener {
	return startCalcServerWithTimeout(t, defaultReadTimeout)
}

// startCalcServerWithTimeout is startCalcServer with an injectable read
// timeout, so timeout behavior can be tested in milliseconds instead of
// waiting on defaultReadTimeout.
func startCalcServerWithTimeout(t *testing.T, readTimeout time.Duration) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConnWithTimeout(conn, readTimeout)
		}
	}()

	return ln
}

// expectClosed reads one byte with a bounded deadline and fails the test if
// the read succeeds - used to assert the server closed its side of the
// connection (EOF or a reset both count) without hanging the test if it
// didn't.
func expectClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if n, err := conn.Read(buf); err == nil {
		t.Fatalf("expected the connection to be closed, got n=%d bytes with no error", n)
	}
}

func dial(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// expectedBytes builds the exact wire bytes httpmsg itself would produce
// for status/body, so these orchestration tests compare against the real
// response contract without re-deriving CRLF/Content-Length rules by hand.
func expectedBytes(t *testing.T, status httpmsg.Status, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := httpmsg.WriteResponse(&buf, httpmsg.NewResponse("HTTP/1.1", status, body)); err != nil {
		t.Fatalf("building expected response: %v", err)
	}
	return buf.Bytes()
}

// readExactly reads exactly len(want) bytes (a bounded deadline guards
// against a hang if the server sends the wrong amount) and compares them.
func readExactly(t *testing.T, conn net.Conn, want []byte) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reading response: %v (want %q)", err, want)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("response mismatch\n got: %q\nwant: %q", got, want)
	}
}

// 1. One request, then the connection is still usable for another.
func TestServeConn_SingleRequestThenConnectionStillUsable(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	conn.Write([]byte("GET /add?a=2&b=3 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "5"))

	// The same socket must still work for another request afterward.
	conn.Write([]byte("GET /add?a=1&b=1 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "2"))
}

// 2 & 5. Multiple sequential requests on the same connection, each written
// and read in strict order - this is both the "different operations get
// correct responses" proof and the "connection persists across requests"
// proof the spec asks for; they are the same underlying mechanism.
func TestServeConn_SequentialRequestsOnSameConnection(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	steps := []struct {
		request string
		status  httpmsg.Status
		body    string
	}{
		{"GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n", httpmsg.StatusOK, "3"},
		{"GET /sub?a=10&b=4 HTTP/1.1\r\nHost: x\r\n\r\n", httpmsg.StatusOK, "6"},
		{"GET /mul?a=6&b=7 HTTP/1.1\r\nHost: x\r\n\r\n", httpmsg.StatusOK, "42"},
		{"GET /div?a=9&b=3 HTTP/1.1\r\nHost: x\r\n\r\n", httpmsg.StatusOK, "3"},
	}

	for _, s := range steps {
		conn.Write([]byte(s.request))
		readExactly(t, conn, expectedBytes(t, s.status, s.body))
	}
}

// 3. Multiple complete requests sent in a single client write must all be
// answered without the server needing another client write.
func TestServeConn_MultipleRequestsBufferedInOneWrite(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	req1 := "GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n"
	req2 := "GET /sub?a=5&b=3 HTTP/1.1\r\nHost: x\r\n\r\n"
	conn.Write([]byte(req1 + req2))

	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "3"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "2"))
}

// 4. A request split across multiple writes must not produce a response
// until it is actually complete.
func TestServeConn_PartialRequestAcrossMultipleWrites(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	conn.Write([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: loc"))

	// Nothing should arrive yet - a short deadline must expire.
	conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatalf("expected no response before the request was completed")
	}

	conn.Write([]byte("alhost\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "3"))
}

// 6. A clean client EOF must not crash or hang the handler, and must leave
// the server able to accept a fresh connection afterward.
func TestServeConn_CleanClientEOFDoesNotAffectServer(t *testing.T) {
	ln := startCalcServer(t)

	first := dial(t, ln)
	first.Write([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, first, expectedBytes(t, httpmsg.StatusOK, "3"))
	first.Close()

	second := dial(t, ln)
	second.Write([]byte("GET /add?a=4&b=5 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, second, expectedBytes(t, httpmsg.StatusOK, "9"))
}

// A malformed-but-framed request must produce a Bad Request response, not
// a crash - the exact malformed-request policy is Phase 8's concern, but
// the loop must already survive it structurally.
func TestServeConn_UnparseableRequestGetsBadRequestNotACrash(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	conn.Write([]byte("NOT A REQUEST LINE\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusBadRequest, httpmsg.StatusBadRequest.ReasonPhrase()))

	// The connection must still be usable afterward.
	conn.Write([]byte("GET /add?a=1&b=1 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "2"))
}

// An incomplete request that never finds a boundary and exceeds
// reqframe.MaxRequestSize must terminate the connection - and must not
// affect the listener's ability to accept a fresh, independent connection.
func TestServeConn_OversizedIncompleteRequestClosesConnection(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	oversized := bytes.Repeat([]byte("x"), reqframe.MaxRequestSize+1)
	conn.Write(oversized)
	expectClosed(t, conn)

	second := dial(t, ln)
	second.Write([]byte("GET /add?a=1&b=1 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, second, expectedBytes(t, httpmsg.StatusOK, "2"))
}

// A request that is missing its final blank line, followed by the client
// closing its write side, must terminate the connection cleanly with no
// response - there is no complete request to route anything to.
func TestServeConn_IncompleteRequestThenEOFTerminatesCleanly(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	conn.Write([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: localhost"))
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
	} else {
		t.Fatalf("expected a *net.TCPConn")
	}

	expectClosed(t, conn)
}

// A connection that sends an incomplete request and then stops entirely
// (no EOF, no more bytes) must eventually be closed by the server's read
// timeout, not held open forever.
func TestServeConn_ReadTimeoutClosesIdleConnection(t *testing.T) {
	const testReadTimeout = 100 * time.Millisecond
	ln := startCalcServerWithTimeout(t, testReadTimeout)
	conn := dial(t, ln)

	conn.Write([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x"))
	expectClosed(t, conn)
}

// --- Phase 9: end-to-end acceptance test ---
//
// wireResponse and readWireResponse deliberately do NOT reuse httpmsg's
// writer/parser: they exist to independently verify what the server
// actually put on the real TCP wire, not to re-check the writer's own
// internal correctness (that's Phase 6's job).

type wireResponse struct {
	version       string
	code          int
	reason        string
	headers       map[string]string
	body          []byte
	contentLength int
	rawHeaderPart []byte // header block through the blank line, as received
}

func writeRequest(t *testing.T, conn net.Conn, req string) {
	t.Helper()
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("writing request: %v", err)
	}
}

// readWireResponse reads one complete response from conn: it reads until
// "\r\n\r\n" is seen (never assuming one Read call delivers the whole
// response), parses the status line and headers, reads exactly
// Content-Length more body bytes, and fails the test if the bytes actually
// read don't match Content-Length exactly.
func readWireResponse(t *testing.T, conn net.Conn) wireResponse {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	var buf []byte
	chunk := make([]byte, 4096)
	headerEnd := -1

	for headerEnd < 0 {
		n, err := conn.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			headerEnd = bytes.Index(buf, []byte("\r\n\r\n"))
		}
		if err != nil && headerEnd < 0 {
			t.Fatalf("reading response headers: %v", err)
		}
	}

	rawHeaderPart := append([]byte{}, buf[:headerEnd+4]...)
	bodySoFar := append([]byte{}, buf[headerEnd+4:]...)

	lines := strings.Split(string(buf[:headerEnd]), "\r\n")
	statusParts := strings.SplitN(lines[0], " ", 3)
	if len(statusParts) != 3 {
		t.Fatalf("malformed status line: %q", lines[0])
	}
	code, err := strconv.Atoi(statusParts[1])
	if err != nil {
		t.Fatalf("malformed status code %q: %v", statusParts[1], err)
	}

	headers := map[string]string{}
	for _, line := range lines[1:] {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("malformed header line: %q", line)
		}
		headers[name] = strings.TrimSpace(value)
	}

	contentLengthStr, ok := headers["Content-Length"]
	if !ok {
		t.Fatalf("response has no Content-Length header")
	}
	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil {
		t.Fatalf("malformed Content-Length %q: %v", contentLengthStr, err)
	}

	for len(bodySoFar) < contentLength {
		n, err := conn.Read(chunk)
		if n > 0 {
			bodySoFar = append(bodySoFar, chunk[:n]...)
		}
		if err != nil && len(bodySoFar) < contentLength {
			t.Fatalf("reading response body: %v", err)
		}
	}
	if len(bodySoFar) != contentLength {
		t.Fatalf("read %d body bytes, Content-Length declared %d (framing bug): %q", len(bodySoFar), contentLength, bodySoFar)
	}

	return wireResponse{
		version:       statusParts[0],
		code:          code,
		reason:        statusParts[2],
		headers:       headers,
		body:          bodySoFar,
		contentLength: contentLength,
		rawHeaderPart: rawHeaderPart,
	}
}

// assertProperCRLF fails the test if raw contains a bare '\n' not preceded
// by '\r' anywhere - proving the server's actual wire bytes use CRLF, not
// merely LF, framing.
func assertProperCRLF(t *testing.T, raw []byte) {
	t.Helper()
	if !bytes.HasSuffix(raw, []byte("\r\n\r\n")) {
		t.Fatalf("response header block does not end with a CRLF blank line: %q", raw)
	}
	for i, b := range raw {
		if b == '\n' && (i == 0 || raw[i-1] != '\r') {
			t.Fatalf("found a bare '\\n' at byte %d not preceded by '\\r': %q", i, raw)
		}
	}
}

func assertResponse(t *testing.T, resp wireResponse, wantCode int, wantReason, wantBody string) {
	t.Helper()
	if resp.version != "HTTP/1.1" {
		t.Errorf("Version = %q, want HTTP/1.1", resp.version)
	}
	if resp.code != wantCode {
		t.Errorf("Status code = %d, want %d", resp.code, wantCode)
	}
	if resp.reason != wantReason {
		t.Errorf("Reason phrase = %q, want %q", resp.reason, wantReason)
	}
	if ct := resp.headers["Content-Type"]; ct != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if conn := resp.headers["Connection"]; conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}
	if len(resp.body) != resp.contentLength {
		t.Errorf("len(body) = %d, Content-Length = %d", len(resp.body), resp.contentLength)
	}
	if string(resp.body) != wantBody {
		t.Errorf("Body = %q, want %q", resp.body, wantBody)
	}
}

// TestServer_EndToEndPersistentConnection is the canonical proof of the
// assignment's central requirement: "build a calculator that stays on the
// line." One real TCP connection carries seven strictly-sequenced
// request/response round trips - four successes, an application error
// (400), an unknown route (404), and a final success - proving the
// connection survives both kinds of error and is still the same live
// connection throughout.
func TestServer_EndToEndPersistentConnection(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	// 1. add
	writeRequest(t, conn, "GET /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp := readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "15")
	assertProperCRLF(t, resp.rawHeaderPart) // raw wire CRLF check (once is enough)

	// 2. sub
	writeRequest(t, conn, "GET /sub?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "5")

	// 3. mul
	writeRequest(t, conn, "GET /mul?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "50")

	// 4. div
	writeRequest(t, conn, "GET /div?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "2")

	// 5. application-level error on a completely well-framed request: 400,
	// connection must stay open.
	writeRequest(t, conn, "GET /add?a=10&b=not-a-number HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 400, "Bad Request", "Bad Request")

	// 6. unknown route: 404, connection must still stay open.
	writeRequest(t, conn, "GET /does-not-exist?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 404, "Not Found", "Not Found")

	// 7. liveness proof: a further valid request succeeds on this exact
	// same TCP connection after two consecutive error responses.
	writeRequest(t, conn, "GET /add?a=100&b=23 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "123")
}

// 405 has no existing real-TCP coverage (only router-level unit tests), so
// this small dedicated test adds it without lengthening the primary
// seven-request sequence above.
func TestServer_MethodNotAllowedOverRealConnection(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	writeRequest(t, conn, "POST /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp := readWireResponse(t, conn)
	assertResponse(t, resp, 405, "Method Not Allowed", "Method Not Allowed")
}
