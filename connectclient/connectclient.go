// Package connectclient is a small, client-only transport for connect-go v2.
//
// It calls unary RPCs with the Connect protocol and binary Protobuf over
// net/http. It implements [connect.Transport], so it works with the clients
// that protoc-gen-connectclient-go generates and with the clients that the
// stock protoc-gen-connect-go v2 plugin generates.
//
// The transport supports deadlines (Connect-Timeout-Ms), error codes and
// details, request and response headers, response trailers, interceptors, and
// gzip-compressed responses through net/http. It does not support streaming,
// the gRPC or gRPC-Web protocols, JSON, request compression, or Connect GET
// requests. Use connectrpc.com/connect/v2/connecthttp for those.
//
// The package exists to keep binaries small: it does not import connecthttp,
// so programs that use it do not link the server, the gRPC protocols, or
// protojson.
package connectclient

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect/v2"
)

// HTTPClient is the part of [*http.Client] that a [Transport] uses.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Transport is a [connect.Transport] that calls unary RPCs with the Connect
// protocol and binary Protobuf.
type Transport struct {
	httpClient   HTTPClient
	baseURL      string
	readMaxBytes int64
	userAgent    string
	header       http.Header
}

var _ connect.Transport = (*Transport)(nil)

// NewTransport returns a [Transport] for the server at baseURL, for example
// "https://api.example.com" or "http://unix" for an HTTP client that dials a
// Unix socket. Options that only apply to a [connect.Client], such as
// [WithInterceptors], are ignored.
func NewTransport(baseURL string, options ...Option) *Transport {
	config := newConfig(options)
	return &Transport{
		httpClient:   config.httpClient,
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		readMaxBytes: config.readMaxBytes,
		userAgent:    config.userAgent,
		header:       config.header,
	}
}

// NewClientStream implements [connect.Transport]. It returns an error with
// [connect.CodeUnimplemented] for streaming RPCs.
func (t *Transport) NewClientStream(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
	if spec.StreamType != connect.StreamTypeUnary {
		return nil, connect.NewError(connect.CodeUnimplemented, "connectclient: only unary RPCs are supported")
	}
	callInfo, _ := connect.CallInfoForClientContext(ctx)
	if callInfo != nil {
		callInfo.Spec = spec
		callInfo.Protocol = connect.ProtocolNameConnect
		callInfo.Codec = connect.CodecNameProto
		callInfo.RequestEncoding = connect.CompressionNameIdentity
		callInfo.ResponseEncoding = connect.CompressionNameIdentity
	}
	return &unaryStream{
		ctx:       ctx,
		transport: t,
		spec:      spec,
		callInfo:  callInfo,
	}, nil
}

// New returns a [*connect.Client] that calls the server at baseURL through a
// [Transport]. Pass the result to a generated client constructor.
func New(baseURL string, options ...Option) *connect.Client {
	config := newConfig(options)
	return connect.NewClient(NewTransport(baseURL, options...), config.interceptors...)
}
