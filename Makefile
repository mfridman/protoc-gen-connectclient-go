.PHONY: build
build:
	@go build -o build/protoc-gen-connectclient-go .
	@./build/protoc-gen-connectclient-go -version

.PHONY: proto
proto: build
	@rm -rf internal/testdata/gen
	@buf generate ./internal/proto --template ./internal/proto/buf.gen.yaml

.PHONY: examples
examples: build
	@rm -rf examples/eliza/gen
	@buf generate buf.build/connectrpc/eliza --template ./examples/eliza/buf.gen.yaml --include-imports

	@rm -rf examples/bestofgo/gen
	@buf generate buf.build/mf192/bestofgo --template ./examples/bestofgo/buf.gen.yaml --include-imports

	@rm -rf examples/buf-api/gen
	@buf generate buf.build/bufbuild/buf --template ./examples/buf-api/buf.gen.yaml --include-imports --path buf/alpha/registry/v1alpha1/authn.proto

.PHONY: generate
generate: proto examples

.PHONY: format
format:
	@buf format -w

.PHONY: gitclean
gitclean:
	@git clean -xdf
