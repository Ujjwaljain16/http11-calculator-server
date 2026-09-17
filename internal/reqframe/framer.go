// Package reqframe isolates complete HTTP-style request blocks from a raw,
// possibly-fragmented byte stream. It knows only where one request ends and
// the next begins ("\r\n\r\n") - it does not parse method, headers, or any
// other request content.
package reqframe

import (
	"bytes"
	"errors"
)

const terminator = "\r\n\r\n"

// MaxRequestSize bounds how many bytes a Framer will accumulate while
// looking for a request boundary that hasn't arrived yet. This subset's
// requests are a one-line GET plus a Host header and maybe one or two
// extras - well under 200 bytes in practice. 8 KiB gives roughly 40x
// headroom over any realistic legitimate request while still bounding how
// much memory an untrusted peer can make a connection hold onto by simply
// never finishing a header block.
const MaxRequestSize = 8 * 1024

// ErrRequestTooLarge is returned by Next when the buffered, still-incomplete
// request has exceeded MaxRequestSize without a request boundary ever being
// found. There is no safely identified request boundary in this case, so
// the caller cannot recover a request from the buffer - it can only give up
// on the connection.
var ErrRequestTooLarge = errors.New("reqframe: buffered request exceeds maximum size before a boundary was found")

// Framer holds a persistent per-connection byte buffer and extracts
// complete requests terminated by "\r\n\r\n" from it.
//
// A single Framer is owned by one connection for that connection's entire
// lifetime; it is not safe for concurrent use.
type Framer struct {
	buf []byte
	// scanned is the length buf had the last time it was searched for the
	// terminator and none was found. The next search resumes from
	// max(0, scanned-3) instead of from the start, since a terminator can
	// never begin more than 3 bytes before the point already ruled out.
	scanned int
}

// NewFramer returns an empty Framer ready to accept bytes.
func NewFramer() *Framer {
	return &Framer{}
}

// Pending reports how many bytes are currently buffered with no complete
// request extracted from them yet. Callers can use this to distinguish an
// idle connection (0 - nothing has arrived since the last request) from
// one with a request already partway in (non-zero), without reaching into
// the Framer's internal buffer.
func (f *Framer) Pending() int {
	return len(f.buf)
}

// Feed appends newly read bytes to the framer's persistent buffer. It does
// no scanning itself; call Next to check for a complete request.
func (f *Framer) Feed(data []byte) {
	f.buf = append(f.buf, data...)
}

// Next attempts to extract exactly one complete request already present in
// the buffer, performing no I/O of its own.
//
//   - Complete request found: returns (bytes, true, nil). The bytes run from
//     the start of the buffer through and including the terminator; any
//     bytes after it are retained internally for the next call.
//   - Nothing complete yet: returns (nil, false, nil). All buffered bytes
//     are kept for a later Feed.
//   - Fatal framing error: returns (nil, false, ErrRequestTooLarge) once the
//     incomplete buffered request exceeds MaxRequestSize. The buffer is left
//     as-is (it is not grown further by Next itself); the caller must treat
//     this as unrecoverable and close the connection, since no request
//     boundary can safely be identified.
//
// The returned slice on success is a copy: mutating the framer afterwards
// (via further Feed/Next calls) can never affect bytes already handed to a
// caller.
func (f *Framer) Next() ([]byte, bool, error) {
	start := f.scanned - (len(terminator) - 1)
	if start < 0 {
		start = 0
	}

	idx := bytes.Index(f.buf[start:], []byte(terminator))
	if idx < 0 {
		f.scanned = len(f.buf)
		if len(f.buf) > MaxRequestSize {
			return nil, false, ErrRequestTooLarge
		}
		return nil, false, nil
	}

	end := start + idx + len(terminator)

	request := make([]byte, end)
	copy(request, f.buf[:end])

	remainder := make([]byte, len(f.buf)-end)
	copy(remainder, f.buf[end:])
	f.buf = remainder
	f.scanned = 0

	return request, true, nil
}
