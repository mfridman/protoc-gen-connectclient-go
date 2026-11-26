EXAMPLES := eliza

.PHONY: build
build:
	@go build -o build/protoc-gen-connectclient-go .
	@./build/protoc-gen-connectclient-go --version

.PHONY: test
test:
	go test -race ./...
	cd internal/integration && go test -race ./...
	cd examples && go vet ./...

.PHONY: lint
lint:
	go vet ./...
	golangci-lint run ./...
	cd internal/integration && golangci-lint run ./...
	buf lint internal/proto

.PHONY: proto
proto: build
	cd internal/integration && buf generate
	buf build internal/proto --as-file-descriptor-set -o internal/testdata/image.binpb

.PHONY: conformance
conformance: build
	cd internal/conformance && buf generate --template buf.gen.yaml
	cd internal/conformance && go build -o ../../build/conformanceclient ./cmd/conformanceclient
	cd internal/conformance && go build -o ../../build/connectconformance connectrpc.com/conformance/cmd/connectconformance
	cd internal/conformance && ../../build/connectconformance --mode client --conf config.yaml -- ../../build/conformanceclient

.PHONY: examples
examples: build
	@for example in $(EXAMPLES); do \
		echo "generate examples/$$example"; \
		buf generate --template examples/$$example/buf.gen.yaml || exit 1; \
	done
	cd examples && go mod tidy

.PHONY: generate
generate: proto examples

.PHONY: format
format:
	buf format -w internal/proto
	golangci-lint fmt ./...

.PHONY: gitclean
gitclean:
	@git clean -xdf
