# Generating code

For each Go package, the plugin writes into a sub-package with the suffix `client`, for example `elizav1client`:

- `<file>.connectclient.go`: a client type for each service, and a procedure constant for each RPC.
- `<package>.connectclient.go`: the `Client` type, `NewClient`, and the code that sends calls.

Streaming RPCs are skipped. Generate all files of a package in one run, as `buf generate` does by default; otherwise `Client` misses services.

## Options

`package_suffix` sets the sub-package suffix. The default is `client`. An empty value (`package_suffix=`) writes into the package of the `.pb.go` files, as in `protoc-gen-connect-go`. The plugin stops with an error if a name then collides, for example with a message named `Client`.

## Other ways to run the plugin

- `go install github.com/mfridman/protoc-gen-connectclient-go@<version>`, then `local: protoc-gen-connectclient-go`.
- `local: [go, run, github.com/mfridman/protoc-gen-connectclient-go@<version>]`.
