# Using the client

`NewClient(baseURL, options...)` returns a `Client` with a field for each service. Each generated package has its own copy of the types below.

| Option | Description |
|---|---|
| `WithHTTPClient` | The HTTP client. The default is `http.DefaultClient`. |
| `WithHeader` | A header for every call, such as `Authorization`. |
| `WithReadMaxBytes` | The largest response. The default is 4 MiB; zero removes the limit. |
| `WithTimeout` | A deadline for calls whose context has none. Without one, a server that does not answer can block a call forever. Zero sets no deadline. |
| `WithHook` | Code that runs around each call. |

The context deadline goes to the server. For retries, pass a retrying client to `WithHTTPClient`, such as `StandardClient()` from [go-retryablehttp](https://github.com/hashicorp/go-retryablehttp).

## Hooks

A hook gets the `*http.Request` and can add headers. The function it returns runs after the call, with the response (nil if none arrived) and the error:

```go
client := apiv1client.NewClient(baseURL, apiv1client.WithHook(
	func(req *http.Request) (func(*http.Response, error), error) {
		start := time.Now()
		return func(res *http.Response, err error) {
			slog.Debug("call", "procedure", req.URL.Path, "duration", time.Since(start), "error", err)
		}, nil
	},
))
```

A hook that returns an error stops the call, and the call returns that error. Hooks cannot change the protocol headers or the context.

## Errors

A failed call returns an `*Error` with the `Procedure`, the Connect `Code` and `Message`, and `Details` (`Details[i].Message()` decodes one):

```go
if callError, ok := errors.AsType[*apiv1client.Error](err); ok && callError.Code == apiv1client.CodeNotFound {
	// ...
}
```

Failed connections, cancellation, and deadlines give `CodeUnavailable`, `CodeCanceled`, and `CodeDeadlineExceeded`, and wrap the cause, so `errors.Is` works.

## Unix sockets

```go
httpClient := &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return new(net.Dialer).DialContext(ctx, "unix", socketPath)
	},
}}
client := apiv1client.NewClient("http://unix", apiv1client.WithHTTPClient(httpClient))
```
