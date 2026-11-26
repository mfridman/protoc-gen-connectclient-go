# Binary size

Bytes added to a program that already uses `net/http` and Protobuf (linux/arm64, `CGO_ENABLED=0`):

| Client | Go 1.27, stripped | Go 1.27, unstripped | Go 1.26, stripped | Go 1.26, unstripped |
|---|---|---|---|---|
| connect-go v2 with `connecthttp` | +1,280 KiB | +1,847 KiB | +960 KiB | +1,345 KiB |
| **protoc-gen-connectclient-go with `connectclient`** | **+128 KiB** | **+189 KiB** | **+256 KiB** | **+382 KiB** |

Stripped sizes change in 64 KiB steps. [`internal/sizetest`](../internal/sizetest) measures this on every test run, and fails if `connectclient` grows past 192 KiB with Go 1.27, or 384 KiB with Go 1.26.

## Why it is small

- `connectclient` does not import `connecthttp`, so the program does not link the server, the gRPC and gRPC-Web protocols, protojson, or the compressors.
- With Go 1.27, `connectclient` decodes Connect error bodies with `encoding/json/jsontext`. The reflection-based `encoding/json` decoder would add about 450 KiB to a program that does not use it already. Go 1.26 has `jsontext` only as an experiment, so there `connectclient` uses `encoding/json`, which costs about 130 KiB more.
- The generated code has no generic types. Each method is a short call to `connect.Client.CallUnary`.
- The generated service clients are structs, not interfaces. The linker removes the methods that a program does not call.
