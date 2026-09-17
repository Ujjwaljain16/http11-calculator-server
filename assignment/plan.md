# HTTP/1.1-Style Calculator Server — Engineering Specification

Go, raw TCP sockets (`net` package), standard library only. No frameworks — in particular, no `net/http`, no third-party routers, no third-party HTTP libraries.

## Part 1 — Executive Understanding

The arithmetic is a decoy. `add`, `sub`, `mul`, `div` are trivial one-liners. The assignment actually tests whether you can:

- Open a raw `net.Listener`/`net.Conn` and read/write bytes correctly.
- Impose an application-level protocol (HTTP-style request/response) on a stream that has no built-in message boundaries.
- Keep a single TCP connection alive across many logical requests — the single hardest and most-graded part.
- Map protocol conditions to correct status codes without crashing the server.

The one sentence that matters most in the brief is: **"Build a calculator that stays on the line."** Everything in this spec is organized around proving the line stays open across six requests, using one TCP handshake.

Deliverable shape: a small, explainable, framework-free Go server + a raw-socket integration test that is the actual proof of correctness. Deadline: before Session 7. You should be able to explain every line at a viva.

## Part 2 — PRD (Product Requirements Document)

**Problem statement.** Build a TCP server that speaks a constrained HTTP/1.1-style subset, exposes four arithmetic operations over GET, and correctly multiplexes multiple sequential requests over one persistent connection.

**Objective.** Demonstrate correct socket-level and application-protocol-level engineering, not calculator functionality.

**Users.** The instructor (grading via PPT + viva); yourself (must be able to explain every design decision live).

**Functional requirements.**

- `GET /add|sub|mul|div?a=<num>&b=<num>` → 200 with plain-text numeric result.
- Division by zero → 400.
- Non-numeric/missing parameters → 400.
- Unknown path/operation → 404.
- Non-GET method → 405.
- Missing `Host` header → 400.
- All of the above must be servable repeatedly over one TCP connection.

**Non-functional requirements.**

- No third-party HTTP/web frameworks, and no use of Go's own `net/http` package — sockets and streams only, via `net.Listen`/`net.Dial` and raw `io.Reader`/`io.Writer`.
- Server must not crash on malformed client input; it keeps accepting new connections.
- Design must be small enough to explain end-to-end in a viva.
- Implementation must be testable via raw-socket integration tests, not just unit tests.

**Scope / out of scope.** See Part 4/5 boundary lists (Section 5 of the brief) — TLS, HTTP/2+, chunked encoding, request bodies, auth, routing frameworks, virtual hosts are explicitly out of scope for the core deliverable.

**Acceptance criteria.** See Part 10.

**Success criteria.** The six-request single-socket test (Part 9) passes, and every status code is triggered by exactly the condition it's supposed to be triggered by, with the connection surviving everything except a genuine framing failure.

## Part 3 — TRD (Technical Requirements / Design)

### 3.1 Key design decisions (resolved up front)

| # | Question | Resolution |
|---|---|---|
| 1 | End of request? | A blank line — literal `\r\n\r\n` — terminates the header block. No request body is required for this subset, so the header block is the entire request. |
| 2 | TCP fragmentation? | A per-connection growable buffer accumulates bytes across reads; boundary search resumes from (last searched offset − 3) so a split `\r\n\r\n` is never missed. |
| 3 | Multiple requests in one read? | After consuming one complete request, re-scan the remaining buffer for another full request before blocking on the next `conn.Read()`. This naturally handles multiple requests arriving in a single TCP read, which also provides the byte-level foundation needed for pipelined request handling — without implementing pipelining as a separate feature. |
| 4 | Response body end (no closing socket)? | `Content-Length`, always. Non-negotiable for persistent connections. |
| 5 | Why Content-Length matters | Without it, a persistent-connection client has no way to know the body has ended short of the connection closing — which defeats persistence entirely. |
| 6 | Persistent HTTP vs. just "keeping a `net.Conn` open"? | Keeping the socket open is necessary but not sufficient — you also need framing discipline (boundary detection) and length-delimited responses so both sides agree on message edges while the pipe stays open. |
| 7 | After a 400? | Connection stays open — a 400 means "I understood where your request ended, but its contents were invalid," which is fully recoverable. |
| 8 | Which malformed requests are connection-safe? | Any request where the framer found `\r\n\r\n` — you know exactly how many bytes were consumed regardless of whether the content parses. Anything where the boundary itself can't be found (or the request line is unparseable garbage even after framing) is not safely recoverable. |
| 9 | When to close? | Client EOF, `Connection: close` (if implemented), an un-recoverable framing failure, or an internal server error while writing a response. |
| 10 | EOF handling? | `Read()` returning `io.EOF` (or any other error mid-read) closes the connection quietly — this is a normal client disconnect, not an error to report. |
| 11 | `Connection: close` in core? | Not required for the core assignment; recommended as the first stretch feature (Part on optional features) since it's cheap and reuses existing close logic. |
| 12 | Chunked encoding? | Not needed. Every response body here has a small, known-in-advance length — `Content-Length` is strictly simpler and sufficient. |
| 13 | Pipelining? | Not explicitly required (the grading test waits for each response). The buffer design in #3 already handles the underlying byte-level case (multiple complete requests sitting in the buffer at once), but that is not the same as implementing pipelining's full semantics (responses must still be returned in request order, which the sequential loop already guarantees) — don't build anything extra for it. |
| 14 | How much HTTP is "enough"? | Request line + headers + blank line on the request side; status line + headers + blank line + body on the response side. No bodies on requests, no chunked/Transfer-Encoding, no content negotiation. |
| 15 | Explicit assumptions | GET-only application logic; no request bodies; ASCII header text; integer arithmetic; single `Host` header expected. |
| 16 | Byte-stream-level tests | Boundary-split test, multi-request-in-one-read test — see Part 9. |
| 17 | Proving one handshake | The integration test opens exactly one `net.Conn` for all six requests and asserts on that single connection object throughout. |
| 18 | Proving the socket is still open after the last response | Send a 7th trivial request (e.g. another `/add`) on the same connection after the six and assert you get a valid response, not a connection-reset/EOF. |
| 19 | Malformed request, connection stays open — what happens next? | Server writes the appropriate 4xx and immediately loops back to read the next request on the same connection. |
| 20 | Minimum safeguards | Cap on accumulated-but-unterminated request size (reject/close before unbounded memory growth); cap on request-line length; non-blocking-forever reads via `conn.SetReadDeadline`. |

### 3.2 Technology stack

Go standard library only: `net` (`net.Listen`, `net.Listener`, `net.Conn`), raw byte-slice buffering built by hand (not `bufio.Scanner` with `bufio.ScanLines`, and not `bufio.Reader.ReadString('\n')` — see Part 12), `strconv` for number parsing, `net/url` only for query-string splitting (parsing `?a=2&b=3` — this is a plain string-splitting utility, not an HTTP framework), Go's built-in `testing` package for all tests (part of the standard library, so it does not violate the "no frameworks" constraint — unlike Java, Go needs no external test dependency at all). Confirm exact Go toolchain version during Phase 0 repo reconnaissance; this repo has Go 1.23.3 installed.

### 3.3 Component responsibilities

See Part 5.

### 3.4 Concurrency model

Recommendation: goroutine-per-connection (Option A) — the direct Go analogue of thread-per-connection.

| Option | Simplicity | Debuggability | Fit for assignment |
|---|---|---|---|
| A. Goroutine-per-connection (`go handleConn(conn)` in the accept loop) | High | High — one goroutine, one connection, linear control flow, trivial to reason about and to `go test -race` | Chosen |
| B. Worker pool (fixed goroutines + a channel of accepted conns) | Medium | Medium | Overkill — the grading test is a single client, single connection |
| C. Single-goroutine, fully synchronous accept loop | Highest | Highest, but can't accept a second connection while one is open | Too limiting even for a demo server |

Goroutines are cheap (a few KB of stack, multiplexed onto OS threads by the Go runtime scheduler), so goroutine-per-connection is both the idiomatic Go pattern and the simplest one to explain in a viva. Because the Go runtime's own scheduler already multiplexes goroutines over OS threads non-blockingly, there is no analogue of "don't build an NIO/selector event loop" to worry about avoiding by hand — the standard library gives you that for free. Don't reach for worker pools, `sync.Pool`, or manual scheduling; that would bury the actual learning objective (framing/boundaries) under unrelated complexity.

### 3.5 Error handling model

Split cleanly into:

- **Client/protocol errors** (bad request line, bad params, unsupported op/method, missing Host) → returned as a Go `error` value (or a typed result) from the parser/router, which the connection loop turns into a proper HTTP response; connection stays open.
- **Server/internal errors** (a write error on the connection, an unexpected panic in routing) → recovered via a per-connection `defer`/`recover`, best-effort logged, that connection closed only. The accept loop and all other connections are unaffected — a panic in one goroutine must never take down `main`.

### 3.6 Resource management

Each connection's `net.Conn` is owned by its handler goroutine and closed via `defer conn.Close()` immediately after it's accepted, so it closes regardless of how the handler function returns — normal EOF, `Connection: close`, or a recovered panic.

## Part 4 — Protocol Specification

### Request grammar

```
METHOD SP REQUEST-TARGET SP HTTP-VERSION CRLF
*( HEADER-NAME ":" SP HEADER-VALUE CRLF )
CRLF
```

No body for this subset. End-of-request = end-of-headers = first bare `CRLF CRLF`.

### Response grammar

```
HTTP-VERSION SP STATUS-CODE SP REASON-PHRASE CRLF
*( HEADER-NAME ":" SP HEADER-VALUE CRLF )
CRLF
BODY
```

BODY length is exactly `Content-Length` bytes — no more, no less.

### Framing rules

- All line terminators are `\r\n`. Never emit or rely on bare `\n`.
- Header block ends at the first occurrence of `\r\n\r\n` in the byte stream.
- The framer operates on raw bytes (`[]byte`), not decoded strings, until a complete header block is isolated — only then convert to a `string` for parsing.
- Request-target for this subset is always `path?query` (e.g. `/add?a=2&b=3`); no absolute-URI form, no CONNECT/OPTIONS/* forms need to be supported (unsupported methods simply fall into the 405 path).

## Part 5 — Architecture & Component Responsibilities

```
Socket layer            → accept connections, own raw net.Conn I/O
        ↓
Request framer          → isolate one complete request's raw bytes from the stream
        ↓
Request parser          → raw bytes → Request struct (method, target, path, query, version, headers)
        ↓
Router / validator      → match path+method to an operation, validate params
        ↓
Calculator service       → pure arithmetic, no HTTP concerns
        ↓
Response model           → status + headers + body, HTTP-agnostic-ish value struct
        ↓
Response writer           → Response struct → correctly framed bytes on the wire
        ↓
Socket layer (write side)
```

### Package layout

```
calcserver/
  go.mod
  cmd/server/main.go              bootstrap: net.Listen, accept loop, go handleConn(conn) per connection
  internal/connhandler/
    handler.go                    owns one net.Conn's lifecycle + the persistent request loop
  internal/httpframe/
    framer.go                     growable buffer + \r\n\r\n boundary detection (the hard part)
    framer_test.go
  internal/httpmsg/
    request.go                    Request struct: method, target, path, queryParams, version, headers
    parser.go                     raw header block → Request
    parser_test.go
    response.go                   Response struct: status, headers, body
    writer.go                     Response → bytes, correct CRLF framing
    writer_test.go
    status.go                     status code + reason phrase constants (200 OK, 400 Bad Request, 404 Not Found, 405 Method Not Allowed)
  internal/router/
    router.go                     path/method → operation dispatch + validation → Response
    router_test.go
  internal/calc/
    calculator.go                 Add/Sub/Mul/Div, overflow-safe, returns error on divide-by-zero
    calculator_test.go
  internal/config/
    config.go                     max header size, read timeout, etc.
  test/integration/
    six_request_test.go           the six-request single-socket proof + 7th liveness request
    boundary_test.go               split-read, multi-request-in-one-write, partial-header-arrival, clean-EOF
  README.md
```

No DI framework, no interfaces beyond what's genuinely useful for tests (e.g. `CalculatorService` can be plain functions — test by calling them directly with real inputs, not by interface substitution, unless you specifically want to unit-test the router against a fake).

## Part 6 — Request/Response Lifecycle (single request, walked through)

1. `connhandler.Handle(conn)` already has a `httpframe.Framer` bound to this connection, with a buffer that persists across requests on this connection.
2. Framer either already has a complete request buffered (leftover from a previous read) or calls `conn.Read(chunk)` into its buffer until `\r\n\r\n` is found.
3. Framer hands the exact header-block bytes to `httpmsg.ParseRequest`, then removes exactly those bytes from its buffer (remainder, if any, stays for the next request).
4. Parser produces a `httpmsg.Request`.
5. Router looks up the path against known operations; if unmatched → 404. If matched but method != `GET` → 405. If matched and `GET` → validate `Host` present → validate `a`/`b` → compute via `calc` package → build 200 response; validation/arithmetic failures build the appropriate 400.
6. `httpmsg.WriteResponse` serializes the `Response` with correct status line, `Content-Length`, `Content-Type: text/plain`, `Connection: keep-alive` (or `close`), blank line, body — and flushes to `conn`.
7. `connhandler.Handle`'s loop returns to step 2 on the same connection, unless the response was tagged "close this connection."

## Part 7 — Persistent Connection & Request-Boundary Design (the core of the assignment)

**Mechanism, precisely:**

- Each connection handler owns a single `httpframe.Framer` instance for the connection's lifetime — not recreated per request. It wraps a resizable byte buffer (a `[]byte` grown with `append`, or a small hand-rolled growable-buffer type) plus a "last scanned offset."
- To get the next request: if the buffer (from previous leftover bytes) already contains `\r\n\r\n`, extract immediately — no socket read needed. This is what correctly handles "multiple requests arrived in one `Read()`."
- Otherwise, call `conn.Read(chunk)` into a temp `[]byte`, append to the buffer, and re-scan for `\r\n\r\n` starting at `max(0, lastScannedLength - 3)` (the "−3" is what makes a boundary split across two reads unmissable — e.g. `...\r\n\r` in read #1, `\n...` in read #2). Use `bytes.Index(buf[start:], []byte("\r\n\r\n"))` for the scan.
- On finding the boundary at index `i`, the complete request is buffer bytes `[0:i+4]`; the framer retains `buf[i+4:]` as the new buffer contents for the next call.
- On `Read()` returning `io.EOF` with an empty/no-pending-request buffer → clean EOF → close connection, return from the handler goroutine.
- On `Read()` returning `io.EOF` (or any error) mid-request (partial bytes buffered, no boundary found) → treat as an abrupt disconnect → close connection, return from the handler goroutine (nothing to respond to).
- A hard cap (e.g. 8 KB, from `internal/config`) on accumulated bytes without finding a boundary → the request is malformed/oversized; because the boundary is genuinely unknown, this is not connection-safe — respond 400 if you can still write to the connection, then close.

Why this satisfies every required behavior in one mechanism: partial reads, multi-request-in-one-read, and split-across-reads are all just different orderings of "buffer, scan from the safe offset, extract when found" — there's no special-casing needed for any of the three.

`Connection: close` (if implemented): the response builder tags the `Response` as "close," `httpmsg.WriteResponse` emits `Connection: close`, and the connection handler returns (closing the connection via its `defer`) immediately after that write instead of looping back to the framer.

## Part 8 — Status-Code / Error Matrix

| Status | Trigger | Body | Key headers | Connection | Notes |
|---|---|---|---|---|---|
| 200 OK | Valid GET on `/add`,`/sub`,`/mul`,`/div` with valid a,b, no divide-by-zero | Numeric result as plain text | `Content-Type: text/plain`, `Content-Length`, `Connection: keep-alive` | Open | — |
| 400 Bad Request | Non-numeric/missing/empty a or b; malformed query syntax; integer overflow on parse or arithmetic; divide-by-zero; missing Host header; request line found via framing but unparseable | Short reason string, e.g. `Bad Request` | Same as above | Open (see exceptions in 3.1 #8) | Divide-by-zero is deliberately 400, not e.g. 422 (per brief) |
| 404 Not Found | Path doesn't match a known operation (e.g. `/pow`) | `Not Found` | Same as above | Open | Method is irrelevant when the path itself is unknown |
| 405 Method Not Allowed | Known path, non-GET method (e.g. `POST /add`) | `Method Not Allowed` | Same as above (`Allow: GET` optional stretch) | Open | — |

**Design decision, called out explicitly (not silently added):** no status codes beyond the four in the brief are introduced (no 422, no 500) — a genuinely unexpected server-side error (or recovered panic) is logged and the connection is closed rather than inventing a 500 response, to keep the status surface exactly matching the spec. If you'd rather have a 500 for internal errors for viva-completeness, that's a one-line addition worth flagging to the instructor as an intentional extra, not something to add silently.

## Part 9 — Testing Strategy

All tests use Go's standard `testing` package (`go test ./...`) — no external test framework needed.

### Unit tests (no sockets)

- `internal/calc/calculator_test.go`: each op, divide-by-zero, overflow, negative numbers.
- `internal/httpmsg/parser_test.go`: well-formed request line/headers → correct `Request`; malformed variants → parser returns a clean error (a sentinel `error` value or wrapped error, not a panic).
- `internal/router/router_test.go`: given constructed `Request`s, correct status/body per case in the query-parameter and routing matrices (Part 8 + brief Section 9).
- `internal/httpmsg/writer_test.go`: byte-exact output for each status, verifying CRLF (not `\n`) and correct `Content-Length`.

### Socket integration tests (the ones that actually prove the assignment)

**The six-request single-socket test** (`test/integration/six_request_test.go`) — the most important test in the whole suite. Open exactly one `net.Conn` (via `net.Dial` against a server started on a random `127.0.0.1` port in the test), send all six requests in the brief's sequence, assert each status/body, then send a 7th request on the same connection to prove it's still alive.

### Boundary/fragmentation tests (byte-stream level, Section 18 of the brief) — `test/integration/boundary_test.go`

- **Split across reads:** write `GET /add?a=2` to the connection, `time.Sleep` briefly, then write the remainder `&b=3 HTTP/1.1\r\nHost: localhost\r\n\r\n` — assert one correct request is parsed, not two, not zero.
- **Multiple requests in one read:** concatenate two full requests into a single `Write()` call — assert both are processed, in order, on subsequent reads from the server's response stream.
- **Sequential request/response:** baseline case — client waits for each response before sending the next; must not regress.
- **Partial header arrival:** send headers split at an arbitrary byte offset across multiple writes.
- **Clean EOF:** client closes the connection after N requests; server must not panic, must simply return from its handler goroutine.

Test matrix (happy path / validation / routing / method / parsing / connection) mirrors the brief's Section 17 — build it as a literal checklist before writing any test code so nothing is missed.

## Part 10 — Acceptance Criteria & Definition of Done

**Acceptance criteria (all must hold):** the four operations work; every status code fires on exactly its documented trigger; the six-request test passes on one TCP connection with the connection still open afterward; boundary tests (split-read, multi-in-one-read) pass; malformed input never crashes the server or the accept loop; resources are cleaned up on every exit path (no leaked goroutines or file descriptors).

**Definition of Done:** source complete · `go vet` clean · unit tests complete · integration tests complete · six-request persistent test passing · all status codes verified by test · boundary tests passing · zero third-party dependencies (`go.mod` has no `require` lines beyond the Go toolchain itself) · README with run instructions, example raw requests, architecture, design decisions, and known limitations · architecture and design decisions documented (this spec + any deltas made during implementation).

## Part 11 — Phased Implementation Roadmap

Each phase below: objective → files → tasks → tests → exit criteria → depends on → common mistakes → don't do yet.

**Phase 0 — Repo reconnaissance.** Objective: know what you're building into. Tasks: identify Go toolchain version, confirm no existing module/package conventions, confirm standard `testing` package will be used. Exit: a short written note of findings, no code changes yet.

**Phase 1 — Minimal TCP server.** Files: `go.mod`, `cmd/server/main.go`. Tasks: `net.Listen("tcp", ...)`, blocking `Accept()` loop, `go` a per-connection handler that reads whatever bytes arrive and echoes them raw. Tests: manual `nc`/PowerShell `Test-NetConnection` or a trivial socket test confirming bytes round-trip. Exit: you can connect and get bytes back. Common mistake: closing the connection after one read. Don't do yet: any HTTP parsing.

**Phase 2 — HTTP request framing.** Files: `internal/httpframe/framer.go`. Tasks: implement the buffer + `\r\n\r\n` scan exactly as in Part 7. Tests: `framer_test.go` feeding the framer byte slices across multiple simulated "reads" (a stub `io.Reader` returning pre-set chunks, e.g. via a small test-only `io.Reader` implementation or `io.MultiReader` over several `bytes.Reader`s), including split-boundary and multi-request-in-one-buffer cases — no real sockets needed yet. Exit: framer correctly isolates request blocks in all boundary cases from Part 9. Depends on: Phase 1. Common mistake: recreating the buffer per request instead of persisting it per connection. Don't do yet: full HTTP parsing (treat the raw block as opaque bytes for now).

**Phase 3 — Request parser.** Files: `internal/httpmsg/request.go`, `parser.go`. Tasks: parse request line (method, target, version) and headers into the `Request` struct; split target into path + query params (`net/url.ParseQuery` is fine here — it's a stdlib string utility, not an HTTP framework). Tests: `parser_test.go` — well-formed and malformed request lines/headers. Exit: correct `Request` for every case in the brief's query-parameter matrix (input side only, no validation logic yet). Depends on: Phase 2.

**Phase 4 — Request model & validation.** Files: validation logic added into `internal/router/router.go`. Tasks: `Host`-presence check, `a`/`b` presence/numeric/overflow checks per the resolutions in Part 3.1's query-parameter decisions (`strconv.ParseInt`/`ParseFloat` with explicit error checks). Tests: full query-parameter matrix from the brief's Section 9, added to `router_test.go`. Exit: validation produces correct pass/fail + reason for every case. Depends on: Phase 3.

**Phase 5 — Calculator routing.** Files: `internal/calc/calculator.go`, `internal/router/router.go`. Tasks: implement `Add`/`Sub`/`Mul`/`Div` (overflow-safe — check bounds explicitly or use `math/big` if you want to be thorough; return an `error` for divide-by-zero), map validated requests to results, unknown path → 404 marker, known path + wrong method → 405 marker. Tests: `calculator_test.go` per operation, `router_test.go` per Part 8 matrix. Exit: `Router` returns a correct in-memory decision (status + body) for every brief scenario, no HTTP bytes yet. Depends on: Phase 4.

**Phase 6 — Response generation.** Files: `internal/httpmsg/response.go`, `writer.go`, `status.go`. Tasks: build byte-exact responses (status line, `Content-Length`, `Content-Type`, `Connection`, blank line, body) with correct CRLF. Tests: `writer_test.go` — byte-exact assertions per status. Exit: writer output matches Part 4's response grammar exactly. Depends on: Phase 5.

**Phase 7 — Persistent connection loop.** Files: `internal/connhandler/handler.go`, `cmd/server/main.go` wiring. Tasks: wire framer → parser → router → writer into a loop that keeps reusing the same `net.Conn` per Part 6. Tests: sequential-request socket test (single request, then a second on the same connection). Exit: two manual requests on one connection both succeed. Depends on: Phases 2–6. Common mistake: closing the connection after the first response.

**Phase 8 — Required error handling.** Tasks: confirm 400/404/405 paths all flow correctly through the persistent loop (not just in isolated router tests). Tests: each error case, asserting the connection stays open afterward per Part 8. Depends on: Phase 7.

**Phase 9 — Integration tests.** Files: `test/integration/six_request_test.go`. Tasks: implement the exact six-request single-socket test from Part 9/brief Section 17, plus the 7th-request liveness check. Exit: test passes reliably, not flaky (`go test -run TestSixRequest -count=5` to check for flakiness). Depends on: Phase 8. This is the test the grading claim rests on — do not skip or thin it out.

**Phase 10 — Boundary/fragmentation tests.** Files: `test/integration/boundary_test.go`. Tasks: implement the five cases from Part 9. Exit: all pass, proving the framer (Phase 2) genuinely doesn't assume read-boundaries-equal-message-boundaries. Depends on: Phase 9 (so you have the full pipeline to test against), though the framer unit tests in Phase 2 should already cover the logic in isolation.

**Phase 11 — Hardening.** Files: `internal/config/config.go`, guards added into `httpframe`/`connhandler`. Tasks only if justified: max request size cap, `conn.SetReadDeadline` (generous, e.g. 60s, so it never trips during grading), clean shutdown of the accept loop (e.g. on `SIGINT` via `os/signal`, optional). Don't do yet: anything not explicitly motivated by Part 3.1 #20's minimum safeguards.

**Phase 12 — Optional features.** Only after Phase 9–10 are green: `Connection: close` support (cheapest, do first if you do any), then idle timeout if genuinely wanted, then (not recommended) chunked/pipelining — skip these last two unless explicitly asked, per Part 3.1 #12/#13.

## Part 12 — Risks, Common Mistakes, Anti-Patterns

- Using `bufio.NewReader(conn).ReadString('\n')` or `bufio.NewScanner(conn)` with `bufio.ScanLines` for framing — these silently assume line-oriented, `\n`-or-`\r\n`-terminated input with library-managed buffering semantics that don't match "stop exactly at `\r\n\r\n`, keep the remainder for the next request." Do your own byte-level buffering (Part 7) instead, only decoding to a `string` once a request block is isolated.
- Assuming one `Read()` = one request or one request = one `Read()` — the entire point of Part 7 is that neither holds; if any test in Part 9's boundary suite is skipped, this bug can hide until grading.
- Recreating the framing buffer per request instead of per connection — silently drops any request bytes that arrived early as part of the next request.
- Closing the connection after any single response — the single highest-weight failure mode given the brief's framing ("build a calculator that stays on the line").
- Forgetting `Content-Length` or getting an off-by-one on it — with a persistent connection this doesn't just look wrong, it actively breaks the next request on the same connection (client mis-reads the boundary of your response).
- Emitting `\n` instead of `\r\n` anywhere in the response — technically off-protocol even if a lenient test client tolerates it (careful with `fmt.Fprintln`/`fmt.Sprintf("...\n")`, which default to bare `\n`).
- Unbounded buffer growth on malformed/never-terminated input — a client that sends bytes with no `\r\n\r\n` and never stops will grow the `[]byte` buffer forever via unchecked `append` without a cap (Part 3.1 #20).
- Blocking forever on a slow/silent client with no `conn.SetReadDeadline`, which can wedge a handler goroutine indefinitely (a leaked-but-idle goroutine, not a crash, but still a resource leak worth guarding against).
- Letting a panic in one connection's handler goroutine propagate uncaught — an unrecovered panic in a goroutine crashes the entire process, taking down the accept loop and every other connection. Always `defer` a `recover()` in the handler.
- Overengineering: reaching for `net/http`, third-party routers, generic routing engines, worker pools, or an abstraction layer/interface per component "just in case" — all explicitly against the brief's stated goal of small + explainable.

## Part 13 — Claude Code Handoff Prompt

Paste the block below into Claude Code once you've reviewed and are happy with this spec.

```
You are implementing the HTTP/1.1-style calculator server described in the attached
engineering specification (plan.md). Do NOT write or modify any code yet.

First:
1. Inspect the repository: Go toolchain version, existing module/package conventions.
2. Compare the current repo state against the spec's PRD/TRD (Parts 2-3) and package
   layout (Part 5).
3. Produce a concrete, repo-specific version of the phased roadmap in Part 11 - exact
   files to create or change per phase, and the exact tests for each phase.
4. Call out any risks or deviations from the spec that the repo forces.
5. Stop and wait for my explicit approval before implementing Phase 1.

Once approved, implement ONE phase at a time. After each phase:
  implement -> go build ./... -> go test ./... for that phase's tests -> show me the diff ->
  explain what changed -> check it against that phase's exit criteria in Part 11 ->
  stop and wait for approval before starting the next phase.

Constraints (non-negotiable, from the spec):
- Go standard library only. No net/http, no third-party routers, no third-party
  HTTP libraries, no third-party test frameworks (use Go's built-in "testing" package).
- The persistent-connection request-boundary mechanism must follow Part 7 exactly:
  a per-connection buffer that persists across requests, scanning for \r\n\r\n from
  (last offset - 3), checking for an already-buffered next request before blocking
  on the next conn.Read().
- The six-request single-socket integration test (Part 9) is the primary correctness
  proof for this assignment - implement it early (Phase 9) and do not let it regress.
- Do not add Connection: close, idle timeouts, chunked encoding, or pipelining support
  unless I explicitly ask for them after the core phases (0-10) are green.
```
