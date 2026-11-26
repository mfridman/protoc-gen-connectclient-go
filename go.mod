module github.com/mfridman/protoc-gen-connectclient-go

go 1.26.0

require (
	connectrpc.com/connect/v2 v2.0.2
	github.com/mfridman/buildversion v0.3.0
	google.golang.org/protobuf v1.36.12
)

tool (
	connectrpc.com/connect/v2/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
