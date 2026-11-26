module github.com/mfridman/protoc-gen-connectclient-go/examples

go 1.26.0

replace github.com/mfridman/protoc-gen-connectclient-go => ../

require (
	connectrpc.com/connect/v2 v2.0.2
	github.com/mfridman/protoc-gen-connectclient-go v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.12
)
