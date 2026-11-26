# Using the client

`connectclient.New` returns a `*connect.Client`. All the service clients share it, so they also share its HTTP connections and interceptors. To use one service only, call its constructor, for example `elizav1client.NewElizaServiceClient(client)`.

## Several packages

The plugin writes one `Client` per Go package. To call services from several packages, pass the same `*connect.Client` to each one. They then share HTTP connections, headers, and interceptors:

```go
client := connectclient.New(
	"https://buf.build",
	connectclient.WithHeader("Authorization", "Bearer "+token),
)
ownerClient := ownerv1client.NewClient(client)
moduleClient := modulev1client.NewClient(client)
```

A program only links the packages that it imports.

## Options

| Option | Description |
|---|---|
| `WithHTTPClient(httpClient)` | Sets the HTTP client. The default is `http.DefaultClient`. |
| `WithHeader(name, value)` | Adds a request header to every call, for example `Authorization`. |
| `WithInterceptors(interceptors...)` | Adds `connect.ClientInterceptor` values, for example for auth, logging, or protovalidate. |
| `WithReadMaxBytes(n)` | Limits the size of a response. The default is 4 MiB. Zero removes the limit. |

## Unix sockets

```go
httpClient := &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	},
}}
client := apiv1client.NewClient(connectclient.New(
	"http://unix",
	connectclient.WithHTTPClient(httpClient),
))
```

## Errors

A failed call returns a `*connect.Error`, the same type that connect-go returns. Use `connect.CodeOf(err)` for the code. Error details come back as `connect.ErrorDetail` values; decode them with `connectproto.UnmarshalErrorDetail`.

If a response has no Connect error body, for example a `502` from a proxy, the code comes from the HTTP status, as the Connect protocol specifies. A failed connection gives `unavailable`. A canceled context gives `canceled`, and an expired deadline gives `deadline_exceeded`.

## Deadlines, headers, and trailers

The transport sends the context deadline to the server in the `Connect-Timeout-Ms` header. Request headers, response headers, and trailers use `connect.CallInfo`:

```go
ctx, callInfo := connect.NewClientContext(ctx)
callInfo.RequestHeader().Set("X-Request-Id", requestID)
resp, err := client.ElizaService.Say(ctx, req)
fmt.Println(callInfo.ResponseHeader().Get("X-Server"), callInfo.ResponseTrailer().Get("X-Trailer"))
```

## Stock connect-go code

`connectclient.Transport` works with any client that the stock `protoc-gen-connect-go` v2 plugin generates:

```go
client := elizav1connect.NewElizaServiceClient(
	connect.NewClient(connectclient.NewTransport("https://demo.connectrpc.com")),
)
```

Stock generated clients are interfaces, so a program links all of their methods. The structs from this plugin are smaller.

## Supported features

| Supported | Not supported |
|---|---|
| Unary RPCs | Streaming RPCs (calls fail with `unimplemented`) |
| The Connect protocol | The gRPC and gRPC-Web protocols |
| Binary Protobuf | JSON |
| HTTP/1.1 and HTTP/2, with or without TLS | Connect GET requests |
| gzip responses, decompressed by `net/http` | Compressed requests |
| Deadlines, error codes and details, headers, trailers | |
| Interceptors and `connect.CallInfo` | |

If you need something from the right column, use `connectrpc.com/connect/v2/connecthttp`. Both transports work with the same server, and you can change from one to the other without changing your generated code.
