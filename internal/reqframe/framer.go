// Package reqframe finds request boundaries in a TCP byte stream. A request
// ends at the first "\r\n\r\n" after its start; the package does not
// interpret the request's contents.
package reqframe

import (
	"bytes"
	"errors"
)

const terminator = "\r\n\r\n"

// MaxRequestSize is the largest number of bytes a Framer will hold while
// still waiting for a request boundary. Requests in this server are well
// under 200 bytes, so 8 KiB is generous while still bounding the memory a
// client can consume by never finishing a request.
const MaxRequestSize = 8 * 1024

// ErrRequestTooLarge is returned by Next when more than MaxRequestSize bytes
// have accumulated without a request boundary. Since no boundary was found,
// the remaining stream cannot be split into requests and the caller should
// close the connection.
var ErrRequestTooLarge = errors.New("reqframe: no request boundary within maximum request size")

// A Framer accumulates the bytes read from one connection and extracts
// complete requests from them. It keeps unconsumed bytes between calls, so a
// request may arrive split across reads and several requests may arrive in
// one read. A Framer belongs to a single connection and is not safe for
// concurrent use.
type Framer struct {
	buf []byte
	// scanned is the length of buf at the last search that found no
	// terminator. The next search starts at scanned-3, because a terminator
	// split across two reads can begin at most 3 bytes before that point.
	scanned int
}

// NewFramer returns an empty Framer.
func NewFramer() *Framer {
	return &Framer{}
}

// Pending returns the number of buffered bytes not yet returned as part of a
// request. It is zero when no partial request is held.
func (f *Framer) Pending() int {
	return len(f.buf)
}

// Feed appends data read from the connection to the buffer.
func (f *Framer) Feed(data []byte) {
	f.buf = append(f.buf, data...)
}

// Next extracts the next complete request from the buffer, without reading
// from the connection.
//
// If the buffer contains a terminator, Next returns the bytes up to and
// including the first one and true; any later bytes stay buffered. If it
// does not, Next returns nil and false. If more than MaxRequestSize bytes are
// buffered with no terminator, it returns ErrRequestTooLarge.
//
// The returned slice is a copy and is not affected by later calls.
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
