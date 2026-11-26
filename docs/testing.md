# Testing

- `go test ./...` runs the plugin's golden and unit tests, and unit tests of the client code against `net/http/httptest` servers.
- `cd internal/integration && go test ./...` runs generated clients against connect-go v2 servers, and the [binary size](size.md) tests. It is a separate module, so connect-go is not a dependency of the main module.
- `make conformance` runs the [Connect conformance suite](https://github.com/connectrpc/conformance) against a generated client: Connect protocol, binary Protobuf, unary RPCs, HTTP/1.1 and HTTP/2, with and without TLS. All 220 test cases pass.
- `make generate` regenerates the test code and the [examples](../examples).
