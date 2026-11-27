# protoc-gen-connectclient-go

Small Go clients for [Connect](https://connectrpc.com/) services, for CLIs and other small programs.

- Unary RPCs only. Plain structs, no generics.
- One `NewClient` per package gives you every service in it.
- The generated code imports only the standard library and `google.golang.org/protobuf`.
- It adds at most 64 KiB to a stripped binary. connect-go adds 768 KiB. See [Binary size](docs/size.md).

This is not a replacement for [connect-go](https://github.com/connectrpc/connect-go). Use connect-go if you need streaming, interceptors, the gRPC or gRPC-Web protocols, JSON, or a server.

## Install

```sh
go get -tool github.com/mfridman/protoc-gen-connectclient-go
```

Requires Go 1.26 or newer.

## Use

Add the plugin to your `buf.gen.yaml`:

```yaml
# buf.gen.yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - local: [go, tool, protoc-gen-connectclient-go]
    out: gen
    opt: paths=source_relative
```

Then:

```go
client := elizav1client.NewClient("https://demo.connectrpc.com")
resp, err := client.ElizaService.Say(ctx, &elizav1.SayRequest{Sentence: "Hello"})
```

## Docs

- [Generating code](docs/generating.md)
- [Using the client](docs/client.md)
- [Changelog](CHANGELOG.md)

## License

MIT. See [LICENSE](LICENSE).
