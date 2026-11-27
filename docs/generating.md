# Generating code

## Configure buf

Add the plugin after `protoc-gen-go`, with the same output directory:

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

Then run `buf generate`. For a configuration that generates code from a module on the Buf Schema Registry, see the [Eliza example](../examples/eliza/buf.gen.yaml).

## Output

The plugin adds a client sub-package next to each package of `.pb.go` files:

```text
gen/connectrpc/eliza/v1/
  eliza.pb.go                          # from protoc-gen-go
  elizav1client/
    eliza.connectclient.go             # one client per service in eliza.proto
    elizav1client.connectclient.go     # Client, NewClient, and the shared code
```

- Streaming RPCs are skipped. A comment in the generated code names them.
- Generate all files of a package in one run, as `buf generate` does. If you do not, `Client` does not have all the services.

## Options

| Option | Default | Description |
|---|---|---|
| `package_suffix` | `client` | The suffix of the client sub-package. |

To put the client in the same package as the `.pb.go` files, as `protoc-gen-connect-go` does, set an empty suffix:

```yaml
# buf.gen.yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - local: [go, tool, protoc-gen-connectclient-go]
    out: gen
    opt:
      - paths=source_relative
      - package_suffix=
```

If a generated name then collides with a name in the package, for example a message named `Client`, the plugin stops with an error.

## Other ways to run the plugin

The examples above use `go tool`. Change the `local` value to use a different method:

| Method | `local` value |
|---|---|
| `go get -tool` | `[go, tool, protoc-gen-connectclient-go]` |
| `go install` | `protoc-gen-connectclient-go` |
| `go run` | `[go, run, github.com/mfridman/protoc-gen-connectclient-go@<version>]` |
