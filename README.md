# HTTP/1.1-Style Calculator Server

A calculator server built directly on raw TCP sockets in Go, speaking a small, hand-written HTTP/1.1-style request/response subset. It's an educational project — the arithmetic is trivial; the point is demonstrating, from first principles:

- raw TCP sockets (`net.Listen`, `net.Conn` — no `net/http`)
- TCP stream semantics (a socket delivers bytes, not messages)
- request framing (finding where one request ends and the next begins)
- HTTP-style request parsing
- persistent connections (many requests over one TCP connection)
- response framing (`Content-Length`-delimited bodies)
- HTTP status codes

## Running

```
go run ./cmd/server
```

By default the server listens on an ephemeral port (`:0`) and prints the address it bound to, e.g. `listening on 127.0.0.1:54321`. To pick a specific port:

```
go run ./cmd/server -addr :8080
```

`-addr` accepts any `host:port` value that `net.Listen("tcp", ...)` accepts.

## Testing

```
go test ./...
```

Also useful:

```
go build ./...
go vet ./...
```

## API / Endpoints

Four `GET` endpoints, each taking two signed 64-bit integer query parameters, `a` and `b`:

```
GET /add?a=<integer>&b=<integer>   -> a + b
GET /sub?a=<integer>&b=<integer>   -> a - b
GET /mul?a=<integer>&b=<integer>   -> a * b
GET /div?a=<integer>&b=<integer>   -> a / b  (integer division, truncated toward zero)
```

Example request:

```
GET /add?a=10&b=5 HTTP/1.1
Host: localhost
```

Example response body:

```
15
```

There is no request-body support — every input is a query parameter on the request line.

## HTTP Requirements

- A request's headers end at the first `\r\n\r\n` — that's the request boundary. This subset has no request bodies, so the header block *is* the whole request.
- A `Host` header must be present (its value is not otherwise validated).
- Every response carries `Content-Length`, computed from the response body's actual byte count.
- Every response carries `Connection: keep-alive`.
- Multiple requests can be sent over one TCP connection, one after another (or even buffered ahead of their responses) — the connection is not closed after a single request.
- TCP `Read` boundaries are never treated as request boundaries: one `Read` may deliver part of a request, all of it, or several requests at once, and the server handles all three cases identically.

## Status Codes

| Status | When |
|---|---|
| `200 OK` | A valid calculator request: known operation, `GET`, `Host` present, `a`/`b` both valid integers, and the arithmetic itself succeeds |
| `400 Bad Request` | A malformed-but-completely-framed request; missing/non-numeric `a` or `b`; missing `Host`; division by zero; arithmetic overflow; or an incomplete request that exceeded the size limit before a boundary was ever found (see below) |
| `404 Not Found` | The path isn't one of `/add`, `/sub`, `/mul`, `/div` |
| `405 Method Not Allowed` | A known path requested with a method other than `GET` |

There is no `500` — every internal/socket-level failure is a connection-level event (see below), not an HTTP response.

## Connection / Resource Handling

- Each TCP connection owns exactly one persistent request framer for its whole lifetime.
- Multiple requests already sitting in the framer's buffer are all processed before the server reads more bytes from the socket.
- A request split across multiple reads is retained until it's complete — nothing is discarded or misinterpreted.
- An **incomplete** request is capped at 8 KiB of accumulated bytes; if no request boundary is found by then, the server makes a best-effort attempt to send `400 Bad Request` and then closes the connection. This differs from an ordinary malformed-but-complete request (which gets `400` and stays open): once no boundary can be found, the server can no longer trust where that request ends, so the connection cannot safely be reused.
- A connection that goes idle mid-request (or between requests) without sending anything for 5 seconds is closed. This timer is reset before every read, so it never interrupts an active exchange of requests.
- A read error, a write error, or the client closing its side of the connection all close only that one connection — the server keeps accepting other clients.

## Architecture

```
TCP socket
    |
reqframe.Framer      - finds "\r\n\r\n", buffers partial/multiple requests
    |
httpmsg.ParseRequest  - raw bytes -> method, path, query, headers
    |
validate.Validate    - checks Host presence, parses a/b as integers
    |
router.Route         - path/method -> known operation, or 404/405
    |
calc                 - Add/Sub/Mul/Div, overflow- and div-by-zero-safe
    |
router.Respond       - outcome -> HTTP status + body
    |
httpmsg.WriteResponse - status line, headers, Content-Length, body, all CRLF
    |
same TCP connection (loop back for the next request)
```

Each package owns exactly one concern: `reqframe` never parses HTTP, `httpmsg` never knows about routing or arithmetic, `calc` never knows HTTP exists at all, and `cmd/server` only wires these together — it contains no parsing, validation, or routing logic of its own. It does construct two direct `400` responses itself (for a request that can't be parsed at all, and for a fatal oversized-framing condition — see below), since in both cases there's no parsed request for `router.Route` to work with.

## Design / Educational Point

TCP is a byte stream, not a sequence of messages: one call to `Read` does not correspond to one HTTP request. A request might arrive in several reads, several requests might arrive in a single read, and a request boundary might be split exactly in half by however the network happened to deliver bytes. That's why the server keeps a persistent `Framer` per connection instead of parsing whatever a single `Read` returns — it accumulates bytes across reads and repeatedly scans for `\r\n\r\n` to find where one request ends and the next begins, regardless of how the underlying reads were chopped up.

`Content-Length` matters for the same reason on the way out: it tells the client exactly how many body bytes belong to this response, so the client can tell where the response ends without the connection having to close. That's what makes it possible to send many requests and responses over one connection instead of one-request-per-connection.

## Out of Scope

Intentionally not implemented, to keep this a small, explainable educational server:

- `net/http` or any HTTP framework
- request bodies
- chunked transfer encoding
- `Connection: close` response mode
- HTTP pipelining as a distinct feature
- HTTP/2
- TLS
- authentication
- CGI/FastCGI
- external dependencies (`go.mod` has none)

This is an HTTP/1.1-style server that implements the HTTP/1.1 concepts this assignment requires — it is not a general-purpose, production-grade HTTP/1.1 implementation.
