# Generating code

Run the plugin together with `protoc-gen-go`. A `buf.gen.yaml` example:

```yaml
version: v2
inputs:
  - module: buf.build/connectrpc/eliza
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: example.com/myapp/gen
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - local: [go, tool, protoc-gen-connectclient-go]
    out: gen
    opt: paths=source_relative
```

## Ways to run the plugin

The examples use a [tool dependency](https://go.dev/doc/modules/managing-dependencies#tools), added with `go get -tool github.com/mfridman/protoc-gen-connectclient-go`. Then your `go.mod` pins the plugin version. The generated code does not import this module.

Other ways also work:

- **Install the binary:** run `go install github.com/mfridman/protoc-gen-connectclient-go@<version>`, then use `local: protoc-gen-connectclient-go`.
- **Run with `go run`:** use `local: [go, run, github.com/mfridman/protoc-gen-connectclient-go@<version>]`.

## Output

For the package `connectrpc.eliza.v1`, the plugin writes into the sub-package `elizav1client`:

- `eliza.connectclient.go`: an `ElizaServiceClient` type with one method per unary RPC, plus a constant with the procedure name of each RPC.
- `elizav1client.connectclient.go`: a `Client` type with a field for each service in the package, and the code that sends the calls. The plugin writes the same code into every package, so the generated code needs no runtime package.

The plugin does not generate streaming RPCs. The client type's doc comment lists the RPCs that it skipped.

Generate all the `.proto` files of a package in one run, as `buf generate` does by default. The `Client` type only includes the services that the plugin sees, so a run with `--path` for one file, or one `protoc` run for each file, writes a `Client` type with only some of the services.

## Options

| Option | Default | Description |
|---|---|---|
| `package_suffix` | `client` | The suffix of the sub-package for the generated code. An empty value (`package_suffix=`) writes the code into the package of the `.pb.go` files. The option works as in `protoc-gen-connect-go`. |

With an empty `package_suffix`, a generated name can be the same as a name that `protoc-gen-go` generates, for example for a message named `Client`. The plugin then stops with an error. The name `<Service>Client` is also a name that `protoc-gen-go-grpc` generates, so do not write both into the same package.
