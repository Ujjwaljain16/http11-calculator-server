// Package reqframe isolates complete HTTP-style request blocks from a raw,
// possibly-fragmented byte stream. It knows only where one request ends and
// the next begins ("\r\n\r\n") - it does not parse method, headers, or any
// other request content.
package reqframe

import "bytes"

const terminator = "\r\n\r\n"

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

// Feed appends newly read bytes to the framer's persistent buffer. It does
// no scanning itself; call Next to check for a complete request.
func (f *Framer) Feed(data []byte) {
	f.buf = append(f.buf, data...)
}

// Next attempts to extract exactly one complete request already present in
// the buffer, performing no I/O of its own. If the buffer contains
// "\r\n\r\n", it returns the bytes from the start of the buffer through and
// including that terminator, and true. Any bytes after the terminator are
// retained internally for the next call. If no terminator is present yet,
// it returns nil, false and keeps all buffered bytes for a later Feed.
//
// The returned slice is a copy: mutating the framer afterwards (via further
// Feed/Next calls) can never affect bytes already handed to a caller.
func (f *Framer) Next() ([]byte, bool) {
	start := f.scanned - (len(terminator) - 1)
	if start < 0 {
		start = 0
	}

	idx := bytes.Index(f.buf[start:], []byte(terminator))
	if idx < 0 {
		f.scanned = len(f.buf)
		return nil, false
	}

	end := start + idx + len(terminator)

	request := make([]byte, end)
	copy(request, f.buf[:end])

	remainder := make([]byte, len(f.buf)-end)
	copy(remainder, f.buf[end:])
	f.buf = remainder
	f.scanned = 0

	return request, true
}
