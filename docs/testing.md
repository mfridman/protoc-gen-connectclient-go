# Testing

- `go test ./...` runs unit tests against a connect-go v2 server, golden tests for the plugin, and the [binary size](size.md) tests.
- `make conformance` runs the [Connect conformance suite](https://github.com/connectrpc/conformance) against the transport: Connect protocol, binary Protobuf, unary RPCs, HTTP/1.1 and HTTP/2, with and without TLS. All 220 test cases pass.
- `make generate` regenerates the test code and the [examples](../examples).
