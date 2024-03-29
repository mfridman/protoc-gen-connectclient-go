package golden

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// When writing Protobuf plugins with protogen, it's common to use g.P() to write individual lines
// of Go code. This is fine for small snippets, but it quickly becomes unwieldy for larger blocks of
// code. For this reason, I like to write Go code with annotations that is parsed into a template
// and executed with the relevant data. This allows me to write Go code that is easy to read and
// understand, while still being able to generate large blocks of code.
//
// Super quick feedback loop!

// +++BEGIN TEMPLATE

// Client is a simple HTTP client for a Connect API, supporting only Unary RPCs.
type Client struct {
	httpClient interface {
		Do(*http.Request) (*http.Response, error)
	}
	baseURL       string
	modifyRequest []func(*http.Request) error
	checkError    func(context.Context, error)
	userAgent     string
	common        service

	// +++BEGIN BLOCK
	//
	// {{- range $val := .Services }}
	//   {{ $val }} *{{ $val }}Client
	// {{- end }}
	//
	// +++END BLOCK
}

func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		baseURL:    baseURL,
		// +++BEGIN BLOCK
		//
		// userAgent:  "{{- .UserAgent }}",
		//
		// +++END BLOCK
	}
	for _, opt := range opts {
		opt.apply(c)
	}
	c.common.client = c

	// +++BEGIN BLOCK
	//
	// {{- range $val := .Services }}
	//   c.{{ $val }} = (*{{ $val }}Client)(&c.common)
	// {{- end }}
	//
	// +++END BLOCK
	return c
}

func (c *Client) do(ctx context.Context, req, resp protoreflect.ProtoMessage, procedure string) (retErr error) {
	defer func() {
		if c.checkError != nil {
			c.checkError(ctx, retErr)
		}
	}()
	by, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+procedure, bytes.NewReader(by))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/proto")
	httpRequest.Header.Set("Accept-Encoding", "gzip")
	if c.userAgent != "" {
		httpRequest.Header.Set("User-Agent", c.userAgent)
	}
	for _, f := range c.modifyRequest {
		if err := f(httpRequest); err != nil {
			return fmt.Errorf("failed to modify request: %w", err)
		}
	}
	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer httpResponse.Body.Close()

	var readCloser io.ReadCloser
	switch httpResponse.Header.Get("Content-Encoding") {
	case "gzip":
		readCloser, err = gzip.NewReader(httpResponse.Body)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer readCloser.Close()
	default:
		readCloser = httpResponse.Body
	}
	data, err := io.ReadAll(readCloser)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	if httpResponse.StatusCode != http.StatusOK {
		if httpResponse.Header.Get("Content-Type") != "application/json" {
			return &HTTPError{
				Procedure: procedure,
				Code:      httpResponse.StatusCode,
			}
		}
		var httpErr struct {
			Code    string
			Message string
		}
		if err := json.Unmarshal(data, &httpErr); err != nil {
			return fmt.Errorf("failed to unmarshal connect error: %w", err)
		}
		return &HTTPError{
			Procedure:   procedure,
			Code:        httpResponse.StatusCode,
			ConnectCode: httpErr.Code,
			Message:     httpErr.Message,
		}
	}
	if err := proto.Unmarshal(data, resp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return nil
}

type ClientOption interface{ apply(*Client) }

type clientOptionFunc func(*Client)

func (f clientOptionFunc) apply(c *Client) { f(c) }

// WithHTTPClient sets the HTTP client used by the client. If not set, http.DefaultClient is used.
func WithHTTPClient(httpClient interface {
	Do(*http.Request) (*http.Response, error)
}) ClientOption {
	return clientOptionFunc(func(c *Client) { c.httpClient = httpClient })
}

// WithModifyRequest sets a function that modifies the HTTP request before it is sent but after it
// is created. This can be used to set additional headers or modify the body.
func WithModifyRequest(f func(*http.Request) error) ClientOption {
	return clientOptionFunc(func(c *Client) {
		c.modifyRequest = append(c.modifyRequest, f)
	})
}

// WithCheckError sets a deferred function that may be called at any time in the client's lifecycle.
// The function is called with the context and an error, which may or may not be nil.
func WithCheckError(f func(context.Context, error)) ClientOption {
	return clientOptionFunc(func(c *Client) {
		c.checkError = f
	})
}

// HTTPError is an error returned by the client when a non-200 HTTP status code is received.
type HTTPError struct {
	Procedure   string
	Code        int
	ConnectCode string
	Message     string
}

func (e *HTTPError) Error() string {
	var msg string
	if e.ConnectCode != "" {
		msg = fmt.Sprintf(" (%s)", e.ConnectCode)
	}
	if e.Message != "" {
		if msg != "" {
			msg += ": "
		} else {
			msg = ": "
		}
		msg += e.Message
	}

	return fmt.Sprintf("HTTP error: %s: %d%s", path.Base(e.Procedure), e.Code, msg)
}

type service struct{ client *Client }

// +++END TEMPLATE
