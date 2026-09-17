package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"calcserver/internal/httpmsg"
)

// startCalcServer runs the same accept-loop/serveConn logic main() uses,
// on a real ephemeral TCP port, so tests exercise the actual orchestration.
func startCalcServer(t *testing.T) net.Listener {
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
			go serveConn(conn)
		}
	}()

	return ln
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
