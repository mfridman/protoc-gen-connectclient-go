# Binary size

Bytes added to a stripped linux/arm64 program that already uses `net/http`, Protobuf, and `encoding/json`:

| | Go 1.27 | Go 1.26 |
|---|---|---|
| connect-go v2 | 768 KiB | 768 KiB |
| protoc-gen-connectclient-go | under 64 KiB | 64 KiB |
| `encoding/json`, if the program does not use it yet | about 450 KiB | about 130 KiB |

Both clients import `encoding/json`. `internal/integration/sizetest` checks the size on every test run.
