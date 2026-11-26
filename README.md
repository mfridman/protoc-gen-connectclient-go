# protoc-gen-connectclient-go

Small, self-contained Go clients for [Connect](https://connectrpc.com/) services.

## Why

The connect-go client supports three protocols, two codecs, compression, and streaming. A program that only makes unary calls still links most of that code.

This plugin generates one client per service, and one `Client` per package that bundles them. The generated code calls the service with a plain HTTP `POST` and binary Protobuf. It depends only on the standard library and `google.golang.org/protobuf`: there is no connect-go and no runtime package to import.

A generated client adds at most 64 KiB, stripped, to a program that already uses `net/http`, Protobuf, and `encoding/json`. connect-go v2 adds 768 KiB. See [Binary size](docs/size.md).

## Install

```sh
go get -tool github.com/mfridman/protoc-gen-connectclient-go
```

Requires Go 1.26 or newer.

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

Then call the service:

```go
client := elizav1client.NewClient("https://demo.connectrpc.com")
resp, err := client.ElizaService.Say(ctx, &elizav1.SayRequest{Sentence: "Hello"})
```

Deadlines go to the server, and errors carry the Connect code, message, and details.

## Docs

- [Generating code](docs/generating.md)
- [Using the client](docs/client.md)
- [Binary size](docs/size.md)
- [Changelog](CHANGELOG.md)
- [Examples](examples)

## License

MIT. See [LICENSE](LICENSE).
