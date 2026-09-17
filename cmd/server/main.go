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

// defaultReadTimeout bounds how long a connection can sit idle waiting for
// the next byte of a request. It is refreshed before every read, so an
// actively-progressing exchange of many requests never trips it - only a
// connection that stops sending mid-request (or between requests) does.
const defaultReadTimeout = 5 * time.Second

func main() {
	addr := flag.String("addr", ":0", "TCP address to listen on (host:port); :0 picks an ephemeral free port")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("listening on %s\n", ln.Addr())

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
	serveConnWithTimeout(conn, defaultReadTimeout)
}

// serveConnWithTimeout is serveConn with an injectable read timeout, so
// tests can exercise timeout behavior in milliseconds instead of waiting on
// the production default.
func serveConnWithTimeout(conn net.Conn, readTimeout time.Duration) {
	defer conn.Close()

	framer := reqframe.NewFramer()
	readBuf := make([]byte, readChunkSize)

	for {
		if !drainBufferedRequests(conn, framer) {
			return
		}

		conn.SetReadDeadline(time.Now().Add(readTimeout))
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
// request grew past reqframe.MaxRequestSize). A fatal framing error has no
// safely identified request boundary, so nothing is written back - the
// connection is simply closed by the caller.
func drainBufferedRequests(conn net.Conn, framer *reqframe.Framer) bool {
	for {
		raw, ok, err := framer.Next()
		if err != nil {
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
// returns false only when writing the response itself fails, which is a
// connection-level failure the caller must act on.
func handleRequest(conn net.Conn, raw []byte) bool {
	req, err := httpmsg.ParseRequest(raw)
	if err != nil {
		resp := httpmsg.NewResponse(fallbackVersion, httpmsg.StatusBadRequest, httpmsg.StatusBadRequest.ReasonPhrase())
		return writeResponse(conn, resp)
	}

	result := validate.Validate(req)
	decision := router.Route(req, result)
	resp := router.Respond(decision, req.Version)
	return writeResponse(conn, resp)
}

func writeResponse(conn net.Conn, resp httpmsg.Response) bool {
	if err := httpmsg.WriteResponse(conn, resp); err != nil {
		log.Printf("write response: %v", err)
		return false
	}
	return true
}
