package reqframe_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"calcserver/internal/reqframe"
)

// next is a small test helper: it fails the test on an unexpected framing
// error, so every existing (non-size-limit) test can keep asserting only
// on (bytes, ok) as before.
func next(t *testing.T, f *reqframe.Framer) ([]byte, bool) {
	t.Helper()
	got, ok, err := f.Next()
	if err != nil {
		t.Fatalf("unexpected framing error: %v", err)
	}
	return got, ok
}

// 1. Terminator arrives in a single Feed.
func TestNext_TerminatorInOneFeed(t *testing.T) {
	f := reqframe.NewFramer()
	req := []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	f.Feed(req)

	got, ok := next(t, f)
	if !ok {
		t.Fatalf("expected a complete request, got none")
	}
	if !bytes.Equal(got, req) {
		t.Fatalf("got %q, want %q", got, req)
	}
	if _, ok := next(t, f); ok {
		t.Fatalf("expected no further request buffered")
	}
}

// 2. Boundary split across reads, at every interior position of the
// 4-byte terminator.
func TestNext_BoundarySplitAcrossFeeds(t *testing.T) {
	full := []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	splitPoints := []int{len(full) - 1, len(full) - 2, len(full) - 3, len(full) - 4}

	for _, sp := range splitPoints {
		sp := sp
		t.Run(fmt.Sprintf("split_at_%d", sp), func(t *testing.T) {
			f := reqframe.NewFramer()
			f.Feed(full[:sp])
			if _, ok := next(t, f); ok {
				t.Fatalf("expected no complete request before remainder arrives")
			}
			f.Feed(full[sp:])
			got, ok := next(t, f)
			if !ok {
				t.Fatalf("expected a complete request after remainder arrives")
			}
			if !bytes.Equal(got, full) {
				t.Fatalf("got %q, want %q", got, full)
			}
		})
	}
}

// 2b. The exact fragment sequence given in the spec: "\r", "\n\r", "\n",
// fed as three separate pieces of the same terminator.
func TestNext_BoundarySplitExactExamplesFromSpec(t *testing.T) {
	prefix := []byte("GET / HTTP/1.1\r\nHost: x")
	fragments := [][]byte{[]byte("\r"), []byte("\n\r"), []byte("\n")}

	f := reqframe.NewFramer()
	f.Feed(prefix)
	for i, frag := range fragments {
		if _, ok := next(t, f); ok {
			t.Fatalf("unexpected complete request before terminator finished (fragment %d)", i)
		}
		f.Feed(frag)
	}

	want := append(append([]byte{}, prefix...), []byte("\r\n\r\n")...)
	got, ok := next(t, f)
	if !ok {
		t.Fatalf("expected a complete request after all terminator fragments fed")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 3. Partial headers, then completion.
func TestNext_PartialHeadersThenCompletion(t *testing.T) {
	f := reqframe.NewFramer()
	f.Feed([]byte("GET /add?a=1&b"))
	if _, ok := next(t, f); ok {
		t.Fatalf("expected no complete request from partial headers")
	}

	f.Feed([]byte("=2 HTTP/1.1\r\nHost: x\r\n\r\n"))
	got, ok := next(t, f)
	if !ok {
		t.Fatalf("expected a complete request after remainder appended")
	}
	want := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 4. Multiple complete requests arriving in one Feed.
func TestNext_MultipleRequestsInOneFeed(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")

	f := reqframe.NewFramer()
	f.Feed(append(append([]byte{}, req1...), req2...))

	got1, ok := next(t, f)
	if !ok || !bytes.Equal(got1, req1) {
		t.Fatalf("request 1: got %q ok=%v, want %q", got1, ok, req1)
	}
	got2, ok := next(t, f)
	if !ok || !bytes.Equal(got2, req2) {
		t.Fatalf("request 2: got %q ok=%v, want %q", got2, ok, req2)
	}
	if _, ok := next(t, f); ok {
		t.Fatalf("expected no third request")
	}
}

// 5. Multiple complete requests plus a partial third.
func TestNext_MultipleRequestsPlusPartialThird(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")
	req3Full := []byte("GET /mul?a=2&b=4 HTTP/1.1\r\nHost: z\r\n\r\n")
	req3Partial := req3Full[:len(req3Full)-6]

	f := reqframe.NewFramer()
	combined := append(append(append([]byte{}, req1...), req2...), req3Partial...)
	f.Feed(combined)

	got1, ok := next(t, f)
	if !ok || !bytes.Equal(got1, req1) {
		t.Fatalf("request 1: got %q ok=%v, want %q", got1, ok, req1)
	}
	got2, ok := next(t, f)
	if !ok || !bytes.Equal(got2, req2) {
		t.Fatalf("request 2: got %q ok=%v, want %q", got2, ok, req2)
	}
	if _, ok := next(t, f); ok {
		t.Fatalf("expected request 3 to still be incomplete")
	}

	f.Feed(req3Full[len(req3Partial):])
	got3, ok := next(t, f)
	if !ok || !bytes.Equal(got3, req3Full) {
		t.Fatalf("request 3: got %q ok=%v, want %q", got3, ok, req3Full)
	}
}

// 6. The tail of request 1's terminator and the start of request 2 arrive
// in the same Feed call.
func TestNext_BoundarySplitWithFollowingRequest(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")
	combined := append(append([]byte{}, req1...), req2...)
	splitAt := len(req1) - 2

	f := reqframe.NewFramer()
	f.Feed(combined[:splitAt])
	if _, ok := next(t, f); ok {
		t.Fatalf("expected no complete request before terminator completes")
	}
	f.Feed(combined[splitAt:])

	got1, ok := next(t, f)
	if !ok || !bytes.Equal(got1, req1) {
		t.Fatalf("request 1: got %q ok=%v, want %q", got1, ok, req1)
	}
	got2, ok := next(t, f)
	if !ok || !bytes.Equal(got2, req2) {
		t.Fatalf("request 2: got %q ok=%v, want %q", got2, ok, req2)
	}
}

// 7. Empty and very small inputs.
func TestNext_EmptyAndTinyInputs(t *testing.T) {
	t.Run("zero_bytes", func(t *testing.T) {
		f := reqframe.NewFramer()
		if _, ok := next(t, f); ok {
			t.Fatalf("expected no request from an empty buffer")
		}
	})
	t.Run("one_byte", func(t *testing.T) {
		f := reqframe.NewFramer()
		f.Feed([]byte("G"))
		if _, ok := next(t, f); ok {
			t.Fatalf("expected no request from a single byte")
		}
	})
	t.Run("fewer_than_four_bytes", func(t *testing.T) {
		f := reqframe.NewFramer()
		f.Feed([]byte("\r\n\r"))
		if _, ok := next(t, f); ok {
			t.Fatalf("expected no request from 3 bytes")
		}
	})
	t.Run("arbitrary_bytes_no_terminator", func(t *testing.T) {
		f := reqframe.NewFramer()
		f.Feed([]byte("this is not a terminated request at all, just plain bytes"))
		if _, ok := next(t, f); ok {
			t.Fatalf("expected no request without a terminator")
		}
	})
}

// 8. Byte preservation: arbitrary (including non-ASCII) bytes must survive
// extraction unchanged.
func TestNext_BytePreservationWithArbitraryBytes(t *testing.T) {
	body := []byte{0x00, 0xFF, 0x10, 'a', 'b', 0x7F, 0x80}
	req := append(append([]byte{}, body...), []byte("\r\n\r\n")...)

	f := reqframe.NewFramer()
	f.Feed(req)
	got, ok := next(t, f)
	if !ok {
		t.Fatalf("expected a complete request")
	}
	if !bytes.Equal(got, req) {
		t.Fatalf("got %v, want %v", got, req)
	}
}

// 9. When multiple terminators are present, extraction stops at the first.
func TestNext_StopsAtFirstTerminatorWhenMultiplePresent(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	trailingLookalike := []byte("\r\n\r\nmore-bytes-that-look-like-another-terminator\r\n\r\n")

	f := reqframe.NewFramer()
	f.Feed(append(append([]byte{}, req1...), trailingLookalike...))

	got, ok := next(t, f)
	if !ok {
		t.Fatalf("expected a complete request")
	}
	if !bytes.Equal(got, req1) {
		t.Fatalf("got %q, want %q (must stop at first terminator)", got, req1)
	}
}

// 10. A second request already sitting in the buffer must be retrievable
// without any further Feed call - the core persistent-buffer requirement.
func TestNext_BufferedSecondRequestNeedsNoFurtherFeed(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")

	f := reqframe.NewFramer()
	f.Feed(append(append([]byte{}, req1...), req2...))

	if _, ok := next(t, f); !ok {
		t.Fatalf("expected first request")
	}
	got2, ok := next(t, f)
	if !ok {
		t.Fatalf("expected second request to already be buffered")
	}
	if !bytes.Equal(got2, req2) {
		t.Fatalf("got %q, want %q", got2, req2)
	}
}

// Ownership: an already-extracted request must never change as a result of
// later Feed/Next activity on the same Framer.
func TestNext_ExtractedRequestIsNotCorruptedByLaterFeeds(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")

	f := reqframe.NewFramer()
	f.Feed(req1)
	got1, ok := next(t, f)
	if !ok {
		t.Fatalf("expected first request")
	}
	want1 := append([]byte{}, req1...)

	f.Feed([]byte("GET /mul?a=9&b=9 HTTP/1.1\r\nHost: z\r\n\r\n"))
	f.Feed(bytes.Repeat([]byte("x"), 4096))

	if !bytes.Equal(got1, want1) {
		t.Fatalf("previously extracted request mutated: got %q, want %q", got1, want1)
	}
}

// Property-style check: the same logical request, fragmented at many
// different deterministic split points, must always extract identically.
// Uses only math/rand with a fixed seed - no external dependency.
func TestNext_FragmentationIsOrderIndependent(t *testing.T) {
	req := []byte("GET /div?a=10&b=0 HTTP/1.1\r\nHost: example\r\nX-Extra: value\r\n\r\n")
	rng := rand.New(rand.NewSource(42))

	for trial := 0; trial < 20; trial++ {
		f := reqframe.NewFramer()
		pos := 0
		var got []byte
		var ok bool
		for pos < len(req) {
			chunk := 1 + rng.Intn(5)
			if pos+chunk > len(req) {
				chunk = len(req) - pos
			}
			f.Feed(req[pos : pos+chunk])
			pos += chunk

			if got, ok = next(t, f); ok {
				break
			}
		}
		if !ok {
			t.Fatalf("trial %d: never completed after feeding all %d bytes", trial, len(req))
		}
		if pos != len(req) {
			t.Fatalf("trial %d: completed early at byte %d of %d", trial, pos, len(req))
		}
		if !bytes.Equal(got, req) {
			t.Fatalf("trial %d: got %q, want %q", trial, got, req)
		}
		if _, ok := next(t, f); ok {
			t.Fatalf("trial %d: unexpected extra buffered request", trial)
		}
	}
}

// --- Phase 8: request size limit ---

// An incomplete request that grows past MaxRequestSize without ever
// producing a boundary must report ErrRequestTooLarge, not hang forever
// waiting for more bytes to complete a terminator that may never come.
func TestNext_OversizedIncompleteRequestIsFatal(t *testing.T) {
	f := reqframe.NewFramer()
	f.Feed(bytes.Repeat([]byte("x"), reqframe.MaxRequestSize+1))

	_, ok, err := f.Next()
	if ok {
		t.Fatalf("expected no complete request to be extracted")
	}
	if !errors.Is(err, reqframe.ErrRequestTooLarge) {
		t.Fatalf("expected ErrRequestTooLarge, got %v", err)
	}
}

// A request exactly at the limit, still with no terminator, is also fatal -
// the limit is a hard ceiling on how long the search can keep growing.
func TestNext_ExactlyAtLimitWithNoTerminatorIsFatal(t *testing.T) {
	f := reqframe.NewFramer()
	f.Feed(bytes.Repeat([]byte("x"), reqframe.MaxRequestSize))

	_, ok, err := f.Next()
	if ok {
		t.Fatalf("expected no complete request to be extracted")
	}
	if err != nil {
		t.Fatalf("exactly MaxRequestSize bytes with no terminator should not yet be fatal, got %v", err)
	}

	// One more byte with still no terminator tips it over.
	f.Feed([]byte("x"))
	_, ok, err = f.Next()
	if ok {
		t.Fatalf("expected no complete request to be extracted")
	}
	if !errors.Is(err, reqframe.ErrRequestTooLarge) {
		t.Fatalf("expected ErrRequestTooLarge, got %v", err)
	}
}

// A legitimately large-but-under-the-limit buffer must still work: the
// limit only fires when a boundary is never found, not merely because the
// buffer is large.
func TestNext_LargeButUnderLimitStillCompletes(t *testing.T) {
	body := bytes.Repeat([]byte("x"), reqframe.MaxRequestSize-100)
	req := append(append([]byte{}, body...), []byte("\r\n\r\n")...)

	f := reqframe.NewFramer()
	f.Feed(req)

	got, ok, err := f.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected a complete request")
	}
	if !bytes.Equal(got, req) {
		t.Fatalf("byte mismatch on a large but valid request")
	}
}

// --- Phase 10: TCP fragmentation / request-boundary tests ---
//
// Split-\r\n\r\n coverage (every interior position of the 4-byte
// terminator: "\r|\n\r\n", "\r\n|\r\n", "\r\n\r|\n") is already exhaustively
// covered by TestNext_BoundarySplitAcrossFeeds above; extracted-request
// immutability by TestNext_ExtractedRequestIsNotCorruptedByLaterFeeds; a
// partial third request retained behind two complete ones by
// TestNext_MultipleRequestsPlusPartialThird; and single-request random
// fragmentation by TestNext_FragmentationIsOrderIndependent. None of those
// are duplicated here.

// A full request fed one byte at a time must produce no complete request
// until the very last byte, then exactly the original bytes.
func TestFramer_OneByteAtATime(t *testing.T) {
	req := []byte("GET /add?a=10&b=5 HTTP/1.1\r\nHost: localhost\r\n\r\n")
	f := reqframe.NewFramer()

	for i, b := range req {
		f.Feed([]byte{b})
		if i < len(req)-1 {
			if _, ok := next(t, f); ok {
				t.Fatalf("unexpected complete request after byte %d of %d", i+1, len(req))
			}
		}
	}

	got, ok := next(t, f)
	if !ok {
		t.Fatalf("expected a complete request after the final byte")
	}
	if !bytes.Equal(got, req) {
		t.Fatalf("got %q, want %q", got, req)
	}
	if _, ok := next(t, f); ok {
		t.Fatalf("expected no further request")
	}
}

// The narrowest possible terminator split (3 of 4 bytes in one Feed, the
// final byte plus an entirely new request in the next) must still extract
// both requests correctly. TestNext_BoundarySplitWithFollowingRequest above
// covers a different (half-and-half) split point; this covers the specific
// "\r\n\r | \n"+request2 pattern.
func TestFramer_SplitTerminatorThenNextRequest(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")

	f := reqframe.NewFramer()
	f.Feed(req1[:len(req1)-1])
	if _, ok := next(t, f); ok {
		t.Fatalf("expected no complete request before the terminator's final byte")
	}

	f.Feed(append([]byte{req1[len(req1)-1]}, req2...))

	got1, ok := next(t, f)
	if !ok || !bytes.Equal(got1, req1) {
		t.Fatalf("request 1: got %q ok=%v, want %q", got1, ok, req1)
	}
	got2, ok := next(t, f)
	if !ok || !bytes.Equal(got2, req2) {
		t.Fatalf("request 2: got %q ok=%v, want %q", got2, ok, req2)
	}
}

// Three requests fragmented at a fixed, deterministic (non-random) sequence
// of chunk sizes deliberately not aligned to any request's boundaries must
// still be extracted as exactly [request1, request2, request3], with no
// corruption, duplication, or omission.
func TestFramer_MultipleRequestsAcrossArbitraryFragments(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")
	req3 := []byte("GET /mul?a=6&b=7 HTTP/1.1\r\nHost: z\r\n\r\n")
	combined := append(append(append([]byte{}, req1...), req2...), req3...)

	// Fixed, deterministic chunk sizes (not random, not aligned to any
	// request's length) - chosen only to guarantee frequent, uneven cuts.
	chunkSizes := []int{1, 2, 5, 3, 8, 1, 13, 4, 6, 2, 9, 1, 17, 5}

	f := reqframe.NewFramer()
	var got [][]byte
	pos, sizeIdx := 0, 0
	for pos < len(combined) {
		size := chunkSizes[sizeIdx%len(chunkSizes)]
		sizeIdx++
		if pos+size > len(combined) {
			size = len(combined) - pos
		}
		f.Feed(combined[pos : pos+size])
		pos += size

		for {
			raw, ok := next(t, f)
			if !ok {
				break
			}
			got = append(got, raw)
		}
	}

	want := [][]byte{req1, req2, req3}
	if len(got) != len(want) {
		t.Fatalf("got %d requests, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("request %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// Pending reports 0 on a fresh Framer, grows as unterminated bytes are
// fed, and drops back to 0 once a complete request is fully extracted
// with nothing left over.
func TestFramer_Pending(t *testing.T) {
	f := reqframe.NewFramer()
	if got := f.Pending(); got != 0 {
		t.Fatalf("Pending() on a fresh Framer = %d, want 0", got)
	}

	f.Feed([]byte("GET /add?a=1"))
	if got := f.Pending(); got != len("GET /add?a=1") {
		t.Fatalf("Pending() after partial feed = %d, want %d", got, len("GET /add?a=1"))
	}

	f.Feed([]byte("&b=2 HTTP/1.1\r\nHost: x\r\n\r\n"))
	if _, ok := next(t, f); !ok {
		t.Fatalf("expected a complete request")
	}
	if got := f.Pending(); got != 0 {
		t.Fatalf("Pending() after full extraction = %d, want 0", got)
	}
}
