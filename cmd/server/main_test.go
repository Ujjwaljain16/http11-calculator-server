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

// startCalcServer starts the server on a free local port and returns its
// listener, which is closed when the test ends.
func startCalcServer(t *testing.T) net.Listener {
	return startCalcServerWithTimeouts(t, defaultIdleTimeout, defaultReadTimeout)
}

// startCalcServerWithTimeouts is startCalcServer with custom timeouts, so
// timeout behavior can be tested quickly.
func startCalcServerWithTimeouts(t *testing.T, idleTimeout, readTimeout time.Duration) net.Listener {
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
			go serveConnWithTimeout(conn, idleTimeout, readTimeout)
		}
	}()

	return ln
}

// expectClosed fails the test unless the server closes the connection
// within two seconds.
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

// expectedBytes returns the bytes httpmsg produces for a keep-alive response
// with the given status and body.
func expectedBytes(t *testing.T, status httpmsg.Status, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := httpmsg.WriteResponse(&buf, httpmsg.NewResponse("HTTP/1.1", status, body)); err != nil {
		t.Fatalf("building expected response: %v", err)
	}
	return buf.Bytes()
}

// readExactly reads len(want) bytes from conn and fails the test if they
// differ from want.
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

// One request, then the connection is still usable for another.
func TestServeConn_SingleRequestThenConnectionStillUsable(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	conn.Write([]byte("GET /add?a=2&b=3 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "5"))

	// The same socket must still work for another request afterward.
	conn.Write([]byte("GET /add?a=1&b=1 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "2"))
}

// Several requests for different operations on one connection, each sent
// only after the previous response has been read.
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

// Multiple complete requests sent in a single client write must all be
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

// A request split across multiple writes must not produce a response
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

// A clean client EOF must not crash or hang the handler, and must leave
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
// a crash, and the connection loop must survive it.
func TestServeConn_UnparseableRequestGetsBadRequestNotACrash(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	conn.Write([]byte("NOT A REQUEST LINE\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusBadRequest, httpmsg.StatusBadRequest.ReasonPhrase()))

	// The connection must still be usable afterward.
	conn.Write([]byte("GET /add?a=1&b=1 HTTP/1.1\r\nHost: x\r\n\r\n"))
	readExactly(t, conn, expectedBytes(t, httpmsg.StatusOK, "2"))
}

// An incomplete request larger than reqframe.MaxRequestSize gets a 400
// response with "Connection: close", after which the server closes the
// connection. The server must keep accepting new connections.
func TestServeConn_OversizedIncompleteRequestGetsBadRequestThenCloses(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	oversized := bytes.Repeat([]byte("x"), reqframe.MaxRequestSize+1)
	conn.Write(oversized)

	resp := readWireResponse(t, conn)
	if resp.code != 400 || resp.reason != "Bad Request" {
		t.Fatalf("status = %d %q, want 400 Bad Request", resp.code, resp.reason)
	}
	if string(resp.body) != "Bad Request" {
		t.Fatalf("body = %q, want %q", resp.body, "Bad Request")
	}
	if len(resp.body) != resp.contentLength {
		t.Fatalf("len(body) = %d, Content-Length = %d", len(resp.body), resp.contentLength)
	}
	// Unlike an ordinary malformed request, this connection cannot be
	// reused, so the response says "close".
	if got := resp.headers["Connection"]; got != "close" {
		t.Fatalf("Connection = %q, want close", got)
	}

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

// A request that starts arriving and then stalls must be closed by the read
// timeout. The idle timeout is set very long so that only the read timeout
// can close the connection.
func TestServeConn_ReadTimeoutClosesConnectionMidRequest(t *testing.T) {
	const testReadTimeout = 100 * time.Millisecond
	ln := startCalcServerWithTimeouts(t, time.Hour, testReadTimeout)
	conn := dial(t, ln)

	conn.Write([]byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x"))
	expectClosed(t, conn)
}

// A connection that sends nothing must be closed by the idle timeout. The
// read timeout is set very long so that only the idle timeout can close the
// connection.
func TestServeConn_IdleTimeoutClosesConnectionBeforeAnyRequestStarts(t *testing.T) {
	const testIdleTimeout = 100 * time.Millisecond
	ln := startCalcServerWithTimeouts(t, testIdleTimeout, time.Hour)
	conn := dial(t, ln)

	expectClosed(t, conn)
}

// --- End-to-end tests ---
//
// wireResponse and readWireResponse parse responses from the raw bytes on the
// socket without using httpmsg, so these tests check what the server actually
// sent.

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

// readWireResponse reads one response from conn. It reads until the blank
// line ending the headers, parses the status line and headers, then reads
// Content-Length body bytes. It fails the test if the number of body bytes
// received differs from Content-Length.
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

// assertProperCRLF fails the test unless raw ends with a blank line and every
// '\n' in it is preceded by '\r'.
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

// TestServer_EndToEndPersistentConnection sends seven requests over a single
// TCP connection, reading each response before sending the next: four
// successful calculations, a 400, a 404, and a final successful calculation.
// It shows that the connection stays open after both error responses.
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

	// 5. non-numeric parameter: 400
	writeRequest(t, conn, "GET /add?a=10&b=not-a-number HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 400, "Bad Request", "Bad Request")

	// 6. unknown path: 404
	writeRequest(t, conn, "GET /does-not-exist?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 404, "Not Found", "Not Found")

	// 7. a valid request still works after the two errors
	writeRequest(t, conn, "GET /add?a=100&b=23 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp = readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "123")
}

// A known path requested with a method other than GET gets a 405.
func TestServer_MethodNotAllowedOverRealConnection(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	writeRequest(t, conn, "POST /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp := readWireResponse(t, conn)
	assertResponse(t, resp, 405, "Method Not Allowed", "Method Not Allowed")
}

// --- Fragmented requests ---

// One request sent in four separate writes gets exactly one correct response.
func TestServer_FragmentedRequestOverRealTCP(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	writeRequest(t, conn, "GET /add?a=10")
	writeRequest(t, conn, "&b=5 HTTP/1.1\r\n")
	writeRequest(t, conn, "Host: localhost\r\n")
	writeRequest(t, conn, "\r\n")

	resp := readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "15")
}

// A request missing only its final byte gets no response until that byte
// arrives.
func TestServer_SplitTerminatorOverRealTCP(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	full := "GET /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n"
	writeRequest(t, conn, full[:len(full)-1])

	conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatalf("expected no response before the terminator's final byte arrives")
	}

	writeRequest(t, conn, full[len(full)-1:])
	resp := readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "15")
}

// Two requests sent in three writes whose boundaries do not line up with the
// requests each get the correct response.
func TestServer_MultipleRequestsCrossBoundaryFragmentsOverRealTCP(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	req1 := "GET /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n"
	req2 := "GET /sub?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n"

	writeRequest(t, conn, req1[:10])           // "GET /add?a" - partway into request 1
	writeRequest(t, conn, req1[10:]+req2[:15]) // rest of request 1 + partway into request 2
	writeRequest(t, conn, req2[15:])           // rest of request 2

	resp1 := readWireResponse(t, conn)
	assertResponse(t, resp1, 200, "OK", "15")

	resp2 := readWireResponse(t, conn)
	assertResponse(t, resp2, 200, "OK", "5")
}

// --- Connection: close ---

// A request with "Connection: close" gets a response saying "close", and the
// server then closes the connection.
func TestServer_ClientRequestedCloseIsHonored(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	writeRequest(t, conn, "GET /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	resp := readWireResponse(t, conn)
	if resp.code != 200 || resp.reason != "OK" {
		t.Fatalf("status = %d %q, want 200 OK", resp.code, resp.reason)
	}
	if string(resp.body) != "15" {
		t.Fatalf("body = %q, want 15", resp.body)
	}
	if len(resp.body) != resp.contentLength {
		t.Fatalf("len(body) = %d, Content-Length = %d", len(resp.body), resp.contentLength)
	}
	if got := resp.headers["Connection"]; got != "close" {
		t.Fatalf("Connection = %q, want close", got)
	}

	expectClosed(t, conn)
}

// Without "Connection: close", the connection stays open.
func TestServer_NoConnectionCloseHeaderStaysOpen(t *testing.T) {
	ln := startCalcServer(t)
	conn := dial(t, ln)

	writeRequest(t, conn, "GET /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp := readWireResponse(t, conn)
	assertResponse(t, resp, 200, "OK", "15")

	writeRequest(t, conn, "GET /add?a=1&b=1 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	resp2 := readWireResponse(t, conn)
	assertResponse(t, resp2, 200, "OK", "2")
}
