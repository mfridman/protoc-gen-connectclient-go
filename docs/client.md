# Using the client

`NewClient(baseURL, options...)` returns a `Client` with a field for each service. Each generated package has its own copy of the types below.

| Option | Default | Description |
|---|---|---|
| `WithHTTPClient` | `http.DefaultClient` | The HTTP client. For retries, use a retrying client, such as `StandardClient()` from [go-retryablehttp](https://github.com/hashicorp/go-retryablehttp). |
| `WithHeader` | none | A header for every call, such as `Authorization`. |
| `WithReadMaxBytes` | 4 MiB | The largest response. Zero removes the limit. |
| `WithTimeout` | none | A deadline for calls whose context has none. Zero sets no deadline. |
| `WithHook` | none | Code that runs around each call. See [Hooks](#hooks). |

The context deadline goes to the server.

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

A failed call returns an `*Error`:

```go
if callError, ok := errors.AsType[*apiv1client.Error](err); ok && callError.Code == apiv1client.CodeNotFound {
	// callError also has Procedure, Message, and Details.
	// callError.Details[i].Message() decodes a detail.
}
```

Errors that do not come from the server wrap the cause, so `errors.Is` works:

| Cause | Code |
|---|---|
| Failed connection | `CodeUnavailable` |
| Canceled context | `CodeCanceled` |
| Deadline exceeded | `CodeDeadlineExceeded` |

## Unix sockets

```go
httpClient := &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return new(net.Dialer).DialContext(ctx, "unix", socketPath)
	},
}}
client := apiv1client.NewClient("http://unix", apiv1client.WithHTTPClient(httpClient))
```
