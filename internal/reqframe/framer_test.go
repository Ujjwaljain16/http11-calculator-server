package reqframe_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"calcserver/internal/reqframe"
)

// next calls f.Next and fails the test if it returns an error.
func next(t *testing.T, f *reqframe.Framer) ([]byte, bool) {
	t.Helper()
	got, ok, err := f.Next()
	if err != nil {
		t.Fatalf("unexpected framing error: %v", err)
	}
	return got, ok
}

// Terminator arrives in a single Feed.
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

// Boundary split across reads, at every interior position of the
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

// A terminator fed as three separate one-to-two-byte pieces: "\r", "\n\r",
// "\n".
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

// Partial headers, then completion.
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

// Multiple complete requests arriving in one Feed.
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

// Multiple complete requests plus a partial third.
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

// The tail of request 1's terminator and the start of request 2 arrive
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

// Empty and very small inputs.
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

// Byte preservation: arbitrary (including non-ASCII) bytes must survive
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

// When multiple terminators are present, extraction stops at the first.
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

// A second request already in the buffer is returned without another Feed.
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

// A returned request is not changed by later Feed or Next calls.
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

// The same request split into random chunks of 1 to 5 bytes (fixed seed, so
// repeatable) is always returned unchanged.
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

// --- Request size limit ---

// More than MaxRequestSize bytes with no terminator gives ErrRequestTooLarge.
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

// Exactly MaxRequestSize bytes is still allowed; one more byte is not.
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

	// One more byte exceeds the limit.
	f.Feed([]byte("x"))
	_, ok, err = f.Next()
	if ok {
		t.Fatalf("expected no complete request to be extracted")
	}
	if !errors.Is(err, reqframe.ErrRequestTooLarge) {
		t.Fatalf("expected ErrRequestTooLarge, got %v", err)
	}
}

// A large request under the limit is returned normally.
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

// --- Fragmentation ---

// A request fed one byte at a time is complete only after its last byte.
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

// The first request's terminator is split so that its last byte arrives in
// the same Feed as the whole second request.
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

// Three requests fed in chunks of varying fixed sizes that do not line up
// with request boundaries are returned exactly and in order.
func TestFramer_MultipleRequestsAcrossArbitraryFragments(t *testing.T) {
	req1 := []byte("GET /add?a=1&b=2 HTTP/1.1\r\nHost: x\r\n\r\n")
	req2 := []byte("GET /sub?a=5&b=3 HTTP/1.1\r\nHost: y\r\n\r\n")
	req3 := []byte("GET /mul?a=6&b=7 HTTP/1.1\r\nHost: z\r\n\r\n")
	combined := append(append(append([]byte{}, req1...), req2...), req3...)

	// Chunk sizes cycle through this list.
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

// Pending is 0 for a new Framer, counts buffered bytes, and returns to 0
// once the only buffered request has been extracted.
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
