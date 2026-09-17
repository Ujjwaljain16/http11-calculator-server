// Phase 1: minimal TCP server that echoes raw bytes back on each connection.
// No HTTP framing/parsing yet - that starts in Phase 2.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
)

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
		go handleConn(conn)
	}
}

// handleConn echoes every byte read from conn back to conn, until the client
// closes the connection or a read/write error occurs.
func handleConn(conn net.Conn) {
	defer conn.Close()
	if _, err := io.Copy(conn, conn); err != nil {
		log.Printf("echo: %v", err)
	}
}
