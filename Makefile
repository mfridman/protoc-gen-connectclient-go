.PHONY: build
build:
	@go build -o build/protoc-gen-connectclient-go .
	@./build/protoc-gen-connectclient-go -version

.PHONY: proto
proto: build
	@rm -rf internal/testdata/gen
	@buf generate ./internal/proto --template ./internal/proto/buf.gen.yaml

.PHONY: examples
examples: build example-bufapi example-eliza example-bestofgo

example-bufapi:
	@buf generate --template ./examples/bufapi/buf.gen.yaml

example-eliza:
	@buf generate --template ./examples/eliza/buf.gen.yaml

example-bestofgo:
	@buf generate --template ./examples/bestofgo/buf.gen.yaml

.PHONY: generate
generate: proto examples

.PHONY: format
format:
	@buf format -w

.PHONY: gitclean
gitclean:
	@git clean -xdf
