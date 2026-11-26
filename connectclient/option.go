package connectclient

import (
	"net/http"

	"connectrpc.com/connect/v2"
)

const (
	defaultReadMaxBytes = 4 << 20 // 4 MiB
	defaultUserAgent    = "connectclient-go"
)

// Option configures a [Transport] or the [*connect.Client] that [New]
// returns.
type Option interface {
	applyToConfig(*config)
}

// WithHTTPClient sets the HTTP client that sends requests. The default is
// [http.DefaultClient]. To call a server on a Unix socket, pass an
// [*http.Client] whose transport dials the socket.
func WithHTTPClient(httpClient HTTPClient) Option {
	return optionFunc(func(config *config) {
		config.httpClient = httpClient
	})
}

// WithReadMaxBytes limits the size of a response body. A larger response
// fails with [connect.CodeResourceExhausted]. The default is 4 MiB, the same
// as connecthttp. A value of zero or less removes the limit.
func WithReadMaxBytes(readMaxBytes int64) Option {
	return optionFunc(func(config *config) {
		config.readMaxBytes = readMaxBytes
	})
}

// WithHeader adds a request header to every call, for example an
// Authorization header or a User-Agent header to replace the default
// "connectclient-go". A header with the same name set on the
// [connect.CallInfo] replaces it for that call. Headers that the protocol
// uses, such as Content-Type, cannot be set.
func WithHeader(name, value string) Option {
	return optionFunc(func(config *config) {
		config.header.Add(name, value)
	})
}

// WithInterceptors adds interceptors to the [*connect.Client] that [New]
// returns. [NewTransport] ignores this option.
func WithInterceptors(interceptors ...connect.ClientInterceptor) Option {
	return optionFunc(func(config *config) {
		config.interceptors = append(config.interceptors, interceptors...)
	})
}

type config struct {
	httpClient   HTTPClient
	readMaxBytes int64
	userAgent    string
	header       http.Header
	interceptors []connect.ClientInterceptor
}

func newConfig(options []Option) *config {
	config := &config{
		httpClient:   http.DefaultClient,
		readMaxBytes: defaultReadMaxBytes,
		userAgent:    defaultUserAgent,
		header:       make(http.Header),
	}
	for _, option := range options {
		option.applyToConfig(config)
	}
	return config
}

type optionFunc func(*config)

func (f optionFunc) applyToConfig(config *config) { f(config) }
