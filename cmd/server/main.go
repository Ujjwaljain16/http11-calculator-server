// Package main wires the calculator server together: one persistent
// reqframe.Framer per TCP connection, feeding httpmsg/validate/router to
// process every request the framer can extract, writing one response per
// request, and returning to the same connection for the next request.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"calcserver/internal/httpmsg"
	"calcserver/internal/reqframe"
	"calcserver/internal/router"
	"calcserver/internal/validate"
)

// fallbackVersion is used only when a request couldn't be parsed at all,
// so its own HTTP-version token isn't available to echo back.
const fallbackVersion = "HTTP/1.1"

// readChunkSize is how many bytes serveConn reads from the socket at a
// time; unrelated to reqframe.MaxRequestSize, which bounds accumulated
// request bytes, not a single read's size.
const readChunkSize = 4096

// defaultReadTimeout bounds how long a read may wait once a request has
// already started arriving (the framer holds some bytes but not yet a full
// request). It is deliberately short: a client that started sending
// something and then stalls is more suspicious than one that simply
// hasn't sent its next request yet.
const defaultReadTimeout = 5 * time.Second

// defaultIdleTimeout bounds how long a read may wait when nothing has
// arrived since the last request completed (the framer is empty) - i.e.
// how long a persistent connection is kept open waiting for a new request
// to even begin. It is more generous than defaultReadTimeout: a client
// legitimately reusing a keep-alive connection may pause between requests
// longer than it should ever stall mid-request. Both are refreshed before
// every read, so an active exchange of many requests never trips either.
const defaultIdleTimeout = 60 * time.Second

func main() {
	addr := flag.String("addr", ":0", "TCP address to listen on (host:port); :0 picks an ephemeral free port")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("listening on %s\n", ln.Addr())

	// On SIGINT/SIGTERM, close the listener so Accept() returns
	// net.ErrClosed and the loop below exits on its own - the same clean
	// shutdown path already used whenever a test closes its listener.
	// In-flight connections are not drained; each is left to finish (or
	// end) on its own goroutine when the process exits.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// A closed listener means there is nothing left to accept and
			// never will be again - keep looping would just spin logging
			// the same error forever. Any other Accept error is treated as
			// transient: log it and keep serving other clients.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("accept: %v", err)
			continue
		}
		go serveConn(conn)
	}
}

// serveConn owns conn for its entire lifetime: one Framer is created here
// and reused across every request on this connection. It drains every
// fully-buffered request before reading more bytes, so a read only ever
// happens when the framer has nothing left to give it.
func serveConn(conn net.Conn) {
	serveConnWithTimeout(conn, defaultIdleTimeout, defaultReadTimeout)
}

// serveConnWithTimeout is serveConn with injectable idle/read timeouts, so
// tests can exercise timeout behavior in milliseconds instead of waiting on
// the production defaults. Before each read, it picks whichever timeout
// applies: idleTimeout if the framer is currently empty (waiting for a new
// request to start), readTimeout if a request is already partway in.
func serveConnWithTimeout(conn net.Conn, idleTimeout, readTimeout time.Duration) {
	defer conn.Close()

	framer := reqframe.NewFramer()
	readBuf := make([]byte, readChunkSize)

	for {
		if !drainBufferedRequests(conn, framer) {
			return
		}

		timeout := idleTimeout
		if framer.Pending() > 0 {
			timeout = readTimeout
		}
		conn.SetReadDeadline(time.Now().Add(timeout))

		n, err := conn.Read(readBuf)
		if n > 0 {
			framer.Feed(readBuf[:n])
		}
		if err != nil {
			// A final chunk may have just completed a request; process it
			// before giving up the connection on EOF, a timeout, or a read
			// error - all three are connection-level failures from here on.
			drainBufferedRequests(conn, framer)
			return
		}
	}
}

// drainBufferedRequests processes every complete request already sitting
// in framer, without touching the socket for reads. It returns false if
// the connection is no longer usable: a response failed to write, or the
// framer reported a fatal framing error (its buffered, still-incomplete
// request grew past reqframe.MaxRequestSize).
//
// A fatal framing error is not an ordinary malformed-request 400: there is
// no safely identified request boundary, so the connection can never be
// reused afterward. As a courtesy, one best-effort 400 is still attempted
// (matching this project's documented design) before closing - whether or
// not that write succeeds, the connection is never read from again.
func drainBufferedRequests(conn net.Conn, framer *reqframe.Framer) bool {
	for {
		raw, ok, err := framer.Next()
		if err != nil {
			// The connection is closing regardless of whether this write
			// succeeds, so the response honestly says so too.
			resp := httpmsg.NewCloseResponse(fallbackVersion, httpmsg.StatusBadRequest, httpmsg.StatusBadRequest.ReasonPhrase())
			_ = writeResponse(conn, resp)
			return false
		}
		if !ok {
			return true
		}
		if !handleRequest(conn, raw) {
			return false
		}
	}
}

// handleRequest runs one framed request through parsing, validation,
// routing, and response construction, then writes the response. It
// returns false when writing the response itself fails (a connection-level
// failure), or when the request itself asked to close the connection via
// its own Connection: close header - in which case the write may have
// succeeded, but the caller must still stop serving this connection.
func handleRequest(conn net.Conn, raw []byte) bool {
	req, err := httpmsg.ParseRequest(raw)
	if err != nil {
		resp := httpmsg.NewResponse(fallbackVersion, httpmsg.StatusBadRequest, httpmsg.StatusBadRequest.ReasonPhrase())
		return writeResponse(conn, resp)
	}

	closeRequested := clientRequestedClose(req)
	result := validate.Validate(req)
	decision := router.Route(req, result)
	resp := router.Respond(decision, req.Version, closeRequested)

	if !writeResponse(conn, resp) {
		return false
	}
	return !closeRequested
}

// clientRequestedClose reports whether req's own Connection header asked
// for the connection to be closed after this response.
func clientRequestedClose(req httpmsg.Request) bool {
	value, ok := req.Headers.Get("Connection")
	return ok && strings.EqualFold(value, "close")
}

func writeResponse(conn net.Conn, resp httpmsg.Response) bool {
	if err := httpmsg.WriteResponse(conn, resp); err != nil {
		log.Printf("write response: %v", err)
		return false
	}
	return true
}
