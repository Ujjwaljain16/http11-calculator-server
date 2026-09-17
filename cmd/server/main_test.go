package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// startEchoListener starts a real TCP listener on an ephemeral port and runs
// the same accept-loop/handleConn logic main() uses, so the test exercises
// the actual Phase 1 server behavior over a real socket.
func startEchoListener(t *testing.T) net.Listener {
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
			go handleConn(conn)
		}
	}()

	return ln
}

func TestEchoServer_ByteForByteOverMultipleWrites(t *testing.T) {
	ln := startEchoListener(t)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	messages := [][]byte{
		[]byte("hello"),
		[]byte("second write"),
		[]byte("third write with more bytes 1234567890"),
	}

	for i, msg := range messages {
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}

		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		got := make([]byte, len(msg))
		if _, err := io.ReadFull(conn, got); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("echo mismatch on write %d: got %q, want %q", i, got, msg)
		}
	}
}

func TestEchoServer_ConcurrentConnectionsAreIndependent(t *testing.T) {
	ln := startEchoListener(t)

	dial := func() net.Conn {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return conn
	}

	connA := dial()
	defer connA.Close()
	connB := dial()
	defer connB.Close()

	msgA := []byte("from-a")
	msgB := []byte("from-b-longer-message")

	if _, err := connA.Write(msgA); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if _, err := connB.Write(msgB); err != nil {
		t.Fatalf("write b: %v", err)
	}

	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	gotA := make([]byte, len(msgA))
	if _, err := io.ReadFull(connA, gotA); err != nil {
		t.Fatalf("read a: %v", err)
	}

	connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	gotB := make([]byte, len(msgB))
	if _, err := io.ReadFull(connB, gotB); err != nil {
		t.Fatalf("read b: %v", err)
	}

	if !bytes.Equal(gotA, msgA) {
		t.Fatalf("connection A echo mismatch: got %q, want %q", gotA, msgA)
	}
	if !bytes.Equal(gotB, msgB) {
		t.Fatalf("connection B echo mismatch: got %q, want %q", gotB, msgB)
	}
}

func TestEchoServer_ClientCloseIsClean(t *testing.T) {
	ln := startEchoListener(t)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len("ping"))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Closing the client side must not hang or panic the server; the listener
	// must still accept a fresh connection afterward.
	conn.Close()

	conn2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial after prior client close: %v", err)
	}
	defer conn2.Close()

	if _, err := conn2.Write([]byte("still alive")); err != nil {
		t.Fatalf("write on second connection: %v", err)
	}
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	got2 := make([]byte, len("still alive"))
	if _, err := io.ReadFull(conn2, got2); err != nil {
		t.Fatalf("read on second connection: %v", err)
	}
	if string(got2) != "still alive" {
		t.Fatalf("echo mismatch: got %q, want %q", got2, "still alive")
	}
}
