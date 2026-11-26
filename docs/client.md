# Using the client

`NewClient(baseURL, options...)` returns a `Client` with a field for each service in the package. The service clients share HTTP connections and options. To use one service only, call its constructor, for example `elizav1client.NewElizaServiceClient(baseURL)`.

Each generated package has its own copy of the client code, so the types below, such as `Error` and `Option`, belong to that package.

## Options

| Option | Description |
|---|---|
| `WithHTTPClient(httpClient)` | Sets the HTTP client. The default is `http.DefaultClient`. |
| `WithHeader(name, value)` | Adds a request header to every call, for example `Authorization` or `User-Agent`. |
| `WithReadMaxBytes(n)` | Limits the size of a response. The default is 4 MiB. Zero removes the limit. |
| `WithAfterCall(hook)` | Runs `hook(ctx, err)` once after each call. `err` is nil on success. |

## Logging and timing

`WithAfterCall` gets the context of the call, so pass what the hook needs, such as a start time or a request ID, in the context:

```go
type startKey struct{}

client := apiv1client.NewClient(baseURL, apiv1client.WithAfterCall(func(ctx context.Context, err error) {
	start, _ := ctx.Value(startKey{}).(time.Time)
	slog.DebugContext(ctx, "call", "duration", time.Since(start), "error", err)
}))
ctx = context.WithValue(ctx, startKey{}, time.Now())
user, err := client.UserService.GetUser(ctx, req)
```

The hook cannot change the result. For retries, per-call headers, or a dump of the HTTP traffic, use an `http.RoundTripper` in the HTTP client.

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

A failed call returns an `*Error` with the `Code`, `Message`, and `Details` from the server, and the `Procedure` that failed. `Error()` returns `code: message`. `CodeOf(err)` returns the code. `Details[i].Message()` unmarshals a detail into its Protobuf type.

If a response has no Connect error body, for example a `502` from a proxy, the code comes from the HTTP status, as the Connect protocol specifies. A failed connection gives `CodeUnavailable`, a canceled context gives `CodeCanceled`, and an expired deadline gives `CodeDeadlineExceeded`. These errors wrap the cause, so `errors.Is(err, syscall.ENOENT)` and `errors.Is(err, context.DeadlineExceeded)` work.

## Deadlines

The client sends the context deadline to the server in the `Connect-Timeout-Ms` header.

## Several packages

Each Go package has its own `Client`. To call services from several packages, create one for each, with the same HTTP client and options:

```go
options := []ownerv1client.Option{ownerv1client.WithHeader("Authorization", "Bearer "+token)}
ownerClient := ownerv1client.NewClient("https://buf.build", options...)
moduleClient := modulev1client.NewClient("https://buf.build", modulev1client.WithHeader("Authorization", "Bearer "+token))
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
| Deadlines, error codes and details, an after-call hook | Interceptors, per-call headers, response headers, and trailers (use an `http.RoundTripper`) |

If you need something from the right column, use [connect-go](https://github.com/connectrpc/connect-go). Both work with the same server.
