# Using the client

`NewClient(baseURL, options...)` returns a `Client` with a field for each service in the package. The service clients share HTTP connections and options.

Each generated package has its own copy of the client code, so the types below, such as `Error` and `Option`, belong to that package.

## Options

| Option | Description |
|---|---|
| `WithHTTPClient(httpClient)` | Sets the HTTP client. The default is `http.DefaultClient`. |
| `WithHeader(name, value)` | Adds a request header to every call, for example `Authorization`, or `User-Agent` to replace the default, such as `connectclient-go/v0.2.0 (go1.27.4)`. |
| `WithReadMaxBytes(n)` | Limits the size of a response. The default is 4 MiB. Zero removes the limit. |
| `WithHook(hook)` | Runs `hook` before each call, and the function it returns after the call. See below. |

## Hooks

A hook runs before each call and gets the `*http.Request`, so it can add headers, for example a refreshed token or a request ID from the context. It can return a `done` function, which runs after the call with the `*http.Response` (nil if none arrived) and the call's error. Keep per-call state, such as a start time or a trace span, in the closure:

```go
client := apiv1client.NewClient(baseURL, apiv1client.WithHook(
	func(req *http.Request) (func(*http.Response, error), error) {
		start := time.Now()
		return func(res *http.Response, err error) {
			slog.DebugContext(req.Context(), "call",
				"procedure", req.URL.Path, "duration", time.Since(start), "error", err)
		}, nil
	},
))
```

- The URL path ends with the procedure, for example `/acme.v1.UserService/GetUser`.
- A hook cannot change the protocol headers, such as `Content-Type`, or the request context.
- Returning an error stops the call. The call returns that error unchanged, not an `*Error`, and the `done` functions of earlier hooks run with it.
- Hooks run in the order they are added, and `done` functions in reverse order, like deferred calls.
- When `done` runs, the response body is already read. Use the status and headers.

For retries or a dump of the HTTP traffic, use an `http.RoundTripper` in the HTTP client.

## Unix sockets

```go
httpClient := &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	},
}}
client := apiv1client.NewClient("http://unix", apiv1client.WithHTTPClient(httpClient))
```

## Errors

A failed call returns an `*Error` with the `Code`, `Message`, and `Details` from the server, and the `Procedure` that failed. `Error()` returns `code: message`. `Details[i].Message()` unmarshals a detail into its Protobuf type. To check the code, use `errors.AsType`:

```go
if callError, ok := errors.AsType[*apiv1client.Error](err); ok && callError.Code == apiv1client.CodeNotFound {
	// ...
}
```

Each generated package has its own `Error` type, so check for the type of the package that made the call.

If a response has no Connect error body, for example a `502` from a proxy, the code comes from the HTTP status, as the Connect protocol specifies. A failed connection gives `CodeUnavailable`, a canceled context gives `CodeCanceled`, and an expired deadline gives `CodeDeadlineExceeded`. These errors wrap the cause, so `errors.Is(err, syscall.ENOENT)` and `errors.Is(err, context.DeadlineExceeded)` work.

## Deadlines

The client sends the context deadline to the server in the `Connect-Timeout-Ms` header.

## Several packages

Each Go package has its own `Client` and its own `Option` type. To call services from several packages, create a client for each, and pass them the same HTTP client to share connections:

```go
httpClient := &http.Client{}
ownerClient := ownerv1client.NewClient("https://buf.build",
	ownerv1client.WithHTTPClient(httpClient),
	ownerv1client.WithHeader("Authorization", "Bearer "+token),
)
moduleClient := modulev1client.NewClient("https://buf.build",
	modulev1client.WithHTTPClient(httpClient),
	modulev1client.WithHeader("Authorization", "Bearer "+token),
)
```

A program only links the packages that it imports.

## Supported features

| Supported | Not supported |
|---|---|
| Unary RPCs | Streaming RPCs (not generated) |
| The Connect protocol | The gRPC and gRPC-Web protocols |
| Binary Protobuf | JSON |
| HTTP/1.1 and HTTP/2, with or without TLS | Connect GET requests |
| gzip responses, decompressed by `net/http` | Compressed requests |
| Deadlines, error codes and details, hooks | Interceptors that change messages |

For retries, use an `http.RoundTripper`. For anything else in the right column, use [connect-go](https://github.com/connectrpc/connect-go). Both work with the same server.
