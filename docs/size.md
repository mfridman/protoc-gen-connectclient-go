# Binary size

Bytes added to a program that already uses `net/http`, Protobuf, and `encoding/json` (linux/arm64, `CGO_ENABLED=0`, Go 1.26 and 1.27):

| Client | Stripped | Unstripped |
|---|---|---|
| connect-go v2 with `connecthttp` | +768 KiB | +1,088 KiB |
| **protoc-gen-connectclient-go** | **under 64 KiB** | **+10 KiB** |

Stripped sizes change in 64 KiB steps. [`internal/integration/sizetest`](../internal/integration/sizetest) measures this on every test run, and fails if a generated client grows past 128 KiB.

A program that does not use `encoding/json` yet pays for it too, because the client decodes Connect error bodies with it: about 450 KiB with Go 1.27, and about 130 KiB with Go 1.26.

## Why it is small

- The generated code has no dependencies beyond the standard library and `google.golang.org/protobuf`. It does not link the connect-go server, the gRPC and gRPC-Web protocols, protojson, or the compressors.
- Each method is a short call to one shared function that sends a `POST`.
- The service clients are structs, not interfaces. The linker removes the methods that a program does not call.
