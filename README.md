# protoc-gen-connectclient-go

Small, client-only Go code for [Connect](https://connectrpc.com/) services, built on [connect-go v2](https://github.com/connectrpc/connect-go).

## Why

The full connect-go client supports three protocols, two codecs, compression, and streaming. A program that only makes unary calls still links most of that code. This project links only what a unary Connect call needs:

- **`protoc-gen-connectclient-go`** generates one client per service, and one `Client` per package that bundles them.
- **`connectclient`** is a small `connect.Transport` for unary calls with the Connect protocol and binary Protobuf.

The result adds about 90% less to a binary than connect-go v2 with `connecthttp`: 128 KiB instead of 1,280 KiB, stripped, with Go 1.27. See [Binary size](docs/size.md).

## Install

```sh
go get -tool github.com/mfridman/protoc-gen-connectclient-go
```

This pins the plugin and the `connectclient` package to the same version in your `go.mod`. Requires Go 1.26 or newer.

## Use

Add the plugin next to `protoc-gen-go` in `buf.gen.yaml`:

```yaml
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - local: [go, tool, protoc-gen-connectclient-go]
    out: gen
    opt: paths=source_relative
```

Run `buf generate`, then `go mod tidy` to add the modules that the generated code imports. Then call the service:

```go
client := elizav1client.NewClient(connectclient.New("https://demo.connectrpc.com"))
resp, err := client.ElizaService.Say(ctx, &elizav1.SayRequest{Sentence: "Hello"})
```

Errors are `*connect.Error` values, and interceptors, deadlines, and headers work as in connect-go.

## Docs

- [Generating code](docs/generating.md)
- [Using the client](docs/client.md)
- [Binary size](docs/size.md)
- [Testing](docs/testing.md)
- [Changelog](CHANGELOG.md)
- [Examples](examples)

## License

MIT. See [LICENSE](LICENSE).
