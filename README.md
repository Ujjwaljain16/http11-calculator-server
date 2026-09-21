# HTTP/1.1-Style Calculator Server

A calculator server written in Go on top of raw TCP sockets. It implements a small HTTP/1.1-style request/response protocol by hand, without `net/http`, and keeps each TCP connection open for any number of requests. The calculation is simple by design. The project is about how a server turns a TCP byte stream into requests and responses:

- TCP delivers a stream of bytes, not messages
- finding request boundaries (request framing)
- parsing requests
- persistent connections
- delimiting responses with `Content-Length`
- HTTP status codes

## Running

```
go run ./cmd/server
```

The server listens on a free port by default and prints the address, for example `listening on [::]:54321`. To choose a port:

```
go run ./cmd/server -addr :8080
```

Press Ctrl+C to stop it. The server stops accepting connections and exits; connections in progress are not drained.

## Testing

```
go test ./...
```

`go build ./...` and `go vet ./...` also pass. The tests include end-to-end tests that connect to the server over real TCP sockets and read the raw response bytes.

## Endpoints

Each endpoint takes two signed 64-bit integers as query parameters `a` and `b`:

| Request | Result |
|---|---|
| `GET /add?a=<int>&b=<int>` | a + b |
| `GET /sub?a=<int>&b=<int>` | a - b |
| `GET /mul?a=<int>&b=<int>` | a * b |
| `GET /div?a=<int>&b=<int>` | a / b, truncated toward zero |

Example request and response:

```
GET /add?a=10&b=5 HTTP/1.1
Host: localhost

HTTP/1.1 200 OK
Content-Type: text/plain
Content-Length: 2
Connection: keep-alive

15
```

Requests have no body; all input is in the query string.

## Protocol

- A request ends at the first blank line, that is, at `\r\n\r\n`.
- A `Host` header is required. Its value is not checked.
- Every response has `Content-Type: text/plain`, a `Content-Length` equal to the body's length in bytes, and a `Connection` header.
- The connection stays open after a response (`Connection: keep-alive`), so a client can send further requests on it. If a request contains `Connection: close`, the response says `Connection: close` and the server then closes the connection.
- Several requests may arrive in one read, and one request may arrive over several reads. The server handles both.

## Status codes

| Status | Cause |
|---|---|
| `200 OK` | A valid request for a known operation with `GET` |
| `400 Bad Request` | Malformed request; missing `Host`; missing or non-integer `a` or `b`; division by zero; result outside the 64-bit range; or a request that exceeds the size limit (see below) |
| `404 Not Found` | Path is not `/add`, `/sub`, `/mul` or `/div` |
| `405 Method Not Allowed` | Known path used with a method other than `GET` |

The server never sends `500`. Socket errors close the affected connection and are not reported to the client.

## Connection handling

- Each connection has its own buffer. Bytes that follow a complete request stay in the buffer for the next request.
- A request that is malformed but complete gets a `400` and the connection stays open, because its end is known.
- If more than 8 KiB arrive without a request boundary, the server cannot tell where the request ends. It sends a `400` with `Connection: close` and closes the connection.
- A connection with no buffered data is closed after 60 seconds without input (idle timeout). A connection with a partly received request is closed after 5 seconds without input (read timeout).
- Read errors, write errors and client disconnects close only the affected connection.

## Structure

```
TCP connection
  -> reqframe.Framer         finds request boundaries in the byte stream
  -> httpmsg.ParseRequest    request text -> method, path, query, headers
  -> validate.Validate       Host present, a and b are integers
  -> router.Route            path and method -> operation, 404, 405 or 400
  -> calc                    add, sub, mul, div with overflow checks
  -> router.Respond          outcome -> status and body
  -> httpmsg.WriteResponse   response -> bytes
  -> same TCP connection
```

| Package | Responsibility |
|---|---|
| `cmd/server` | Listens, accepts connections, runs the read loop and timeouts |
| `internal/reqframe` | Buffers bytes and returns one complete request at a time |
| `internal/httpmsg` | Request parsing, response construction and serialization |
| `internal/validate` | Checks the `Host` header and the `a` and `b` parameters |
| `internal/router` | Chooses the operation or error outcome and builds the response |
| `internal/calc` | Integer arithmetic with overflow and division-by-zero errors |

## Why a persistent buffer

One `Read` on a TCP connection may return part of a request, a whole request, or several requests. The server therefore keeps a buffer per connection and searches it for `\r\n\r\n`. After a request is removed, the bytes after it remain for the next one. The search resumes three bytes before the end of the previously searched data, so a terminator split across two reads is still found.

`Content-Length` serves the same purpose for responses: it tells the client where the body ends, so the connection can carry another response without being closed.

## Not implemented

- request bodies
- chunked transfer encoding
- HTTP pipelining as a separate feature
- HTTP/2 and TLS
- authentication
- `net/http`, any framework, and any external dependency (`go.mod` lists none)

This is an HTTP/1.1-style server covering the parts of HTTP/1.1 needed for this project, not a complete implementation of the standard.
