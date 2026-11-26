# Binary size

Bytes added to a program that already uses `net/http`, Protobuf, and `encoding/json` (linux/arm64, stripped):

| | Go 1.27 | Go 1.26 |
|---|---|---|
| connect-go v2 | 768 KiB | 768 KiB |
| protoc-gen-connectclient-go | under 64 KiB | 64 KiB |

A program without `encoding/json` also pays for it: about 450 KiB (Go 1.27) or 130 KiB (Go 1.26). `internal/integration/sizetest` checks the size on every test run.
