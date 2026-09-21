// Command server is a calculator server built directly on TCP sockets. Each
// accepted connection gets one persistent reqframe.Framer; every complete
// request it yields is parsed, validated, routed and answered on the same
// connection, which stays open for further requests.
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

// fallbackVersion is the HTTP version used in responses to requests that
// could not be parsed, since no version is available to echo back.
const fallbackVersion = "HTTP/1.1"

// readChunkSize is the size of the buffer used for each socket read.
const readChunkSize = 4096

// defaultReadTimeout is how long a read may wait once a request has started
// arriving but is not yet complete.
const defaultReadTimeout = 5 * time.Second

// defaultIdleTimeout is how long a read may wait for a new request to begin
// on a connection with nothing buffered. It is longer than
// defaultReadTimeout because a client may pause between requests.
const defaultIdleTimeout = 60 * time.Second

func main() {
	addr := flag.String("addr", ":0", "TCP address to listen on (host:port); :0 picks a free port")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("listening on %s\n", ln.Addr())

	// On SIGINT or SIGTERM, close the listener so that Accept returns
	// net.ErrClosed and the loop below ends. Connections already being
	// served are not drained.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// A closed listener cannot accept again, so stop. Any other
			// error is treated as temporary.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("accept: %v", err)
			continue
		}
		go serveConn(conn)
	}
}

// serveConn serves conn until it is closed, using the default timeouts.
func serveConn(conn net.Conn) {
	serveConnWithTimeout(conn, defaultIdleTimeout, defaultReadTimeout)
}

// serveConnWithTimeout serves conn until it ends. One Framer is created for
// the connection and reused for every request on it. Requests already
// buffered are all handled before the next socket read. Before each read the
// deadline is set to idleTimeout if the framer holds no bytes, or to
// readTimeout if a request is partly received.
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
			// EOF, timeout or read error. The last chunk read may have
			// completed a request, so handle it before returning.
			drainBufferedRequests(conn, framer)
			return
		}
	}
}

// drainBufferedRequests handles every complete request currently in framer
// without reading from the socket. It reports whether the connection can
// still be used.
//
// If the framer reports ErrRequestTooLarge, no request boundary can be
// located, so the rest of the stream cannot be interpreted. A 400 response
// is attempted and the connection is then closed.
func drainBufferedRequests(conn net.Conn, framer *reqframe.Framer) bool {
	for {
		raw, ok, err := framer.Next()
		if err != nil {
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

// handleRequest parses, validates, routes and answers one framed request. It
// reports whether the connection can still be used: false if the response
// could not be written or the client sent "Connection: close".
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

// clientRequestedClose reports whether req has a "Connection: close" header.
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
