// Package main wires the calculator server together: one persistent
// reqframe.Framer per TCP connection, feeding httpmsg/validate/router to
// process every request the framer can extract, writing one response per
// request, and returning to the same connection for the next request.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"calcserver/internal/httpmsg"
	"calcserver/internal/reqframe"
	"calcserver/internal/router"
	"calcserver/internal/validate"
)

// fallbackVersion is used only when a request couldn't be parsed at all,
// so its own HTTP-version token isn't available to echo back.
const fallbackVersion = "HTTP/1.1"

// readChunkSize is how many bytes serveConn reads from the socket at a
// time; it has nothing to do with the request-size cap Phase 11 will add.
const readChunkSize = 4096

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
	defer conn.Close()

	framer := reqframe.NewFramer()
	readBuf := make([]byte, readChunkSize)

	for {
		if !drainBufferedRequests(conn, framer) {
			return
		}

		n, err := conn.Read(readBuf)
		if n > 0 {
			framer.Feed(readBuf[:n])
		}
		if err != nil {
			// A final chunk may have just completed a request; process it
			// before giving up the connection on EOF or a read error.
			drainBufferedRequests(conn, framer)
			return
		}
	}
}

// drainBufferedRequests processes every complete request already sitting
// in framer, without touching the socket for reads. It returns false if a
// response failed to write, meaning the connection is no longer usable.
func drainBufferedRequests(conn net.Conn, framer *reqframe.Framer) bool {
	for {
		raw, ok := framer.Next()
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
