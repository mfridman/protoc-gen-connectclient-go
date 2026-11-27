// Package clientruntime is the code that protoc-gen-connectclient-go copies
// into every generated package, below the package clause. It calls unary RPCs
// with the Connect protocol and binary Protobuf, using only net/http and
// google.golang.org/protobuf.
//
// It is a normal Go package so that it can be compiled and tested. Generated
// code does not import it.
package clientruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// HTTPClient is the part of [*http.Client] that the client uses.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Option configures a client.
type Option interface {
	applyToConfig(*clientConfig)
}

// WithHTTPClient sets the HTTP client that sends requests. The default is
// [http.DefaultClient]. Use an [http.RoundTripper] for retries, and a custom
// dialer to call a server on a Unix socket.
func WithHTTPClient(httpClient HTTPClient) Option {
	return optionFunc(func(config *clientConfig) {
		config.httpClient = httpClient
	})
}

// WithHeader adds a request header to every call, for example an
// Authorization header, or a User-Agent header to replace the default, such
// as "connectclient-go/v0.2.0 (go1.27.4)".
// Headers that the protocol uses, such as Content-Type, cannot be set.
func WithHeader(name, value string) Option {
	return optionFunc(func(config *clientConfig) {
		config.header.Add(name, value)
	})
}

// WithReadMaxBytes limits the size of a response body. A larger response
// fails with [CodeResourceExhausted]. The default is 4 MiB. A value of zero
// or less removes the limit.
func WithReadMaxBytes(readMaxBytes int64) Option {
	return optionFunc(func(config *clientConfig) {
		config.readMaxBytes = readMaxBytes
	})
}

// WithTimeout sets a deadline for calls whose context has none. The server
// gets the deadline in the Connect-Timeout-Ms header. Without this option or
// a context deadline, a call to a server that does not answer can wait
// forever. A value of zero or less sets no deadline.
func WithTimeout(timeout time.Duration) Option {
	return optionFunc(func(config *clientConfig) {
		config.timeout = timeout
	})
}

// WithHook adds a hook that runs before each call. It can change the HTTP
// request, for example to add headers; the protocol headers are set after the
// hooks run, so a hook cannot change them. Returning an error stops the call,
// and the call returns that error unchanged.
//
// If the hook returns a done function, it runs after the call with the
// response, or nil if none arrived, and the call's error, which is nil on
// success. The response body is already read. Done functions run in the
// reverse order of their hooks, like deferred calls.
//
// The URL path of the request ends with the procedure, for example
// "/acme.v1.UserService/GetUser". The request context is the context of the
// call; a hook cannot replace it.
func WithHook(hook func(req *http.Request) (done func(res *http.Response, err error), err error)) Option {
	return optionFunc(func(config *clientConfig) {
		config.hooks = append(config.hooks, hook)
	})
}

// Code is a Connect error code, such as "not_found".
type Code string

// The Connect error codes. See https://connectrpc.com/docs/protocol#error-codes.
const (
	CodeCanceled           Code = "canceled"
	CodeUnknown            Code = "unknown"
	CodeInvalidArgument    Code = "invalid_argument"
	CodeDeadlineExceeded   Code = "deadline_exceeded"
	CodeNotFound           Code = "not_found"
	CodeAlreadyExists      Code = "already_exists"
	CodePermissionDenied   Code = "permission_denied"
	CodeResourceExhausted  Code = "resource_exhausted"
	CodeFailedPrecondition Code = "failed_precondition"
	CodeAborted            Code = "aborted"
	CodeOutOfRange         Code = "out_of_range"
	CodeUnimplemented      Code = "unimplemented"
	CodeInternal           Code = "internal"
	CodeUnavailable        Code = "unavailable"
	CodeDataLoss           Code = "data_loss"
	CodeUnauthenticated    Code = "unauthenticated"
)

// Error is the error that a failed call returns, unless a hook stops the
// call. Errors from the server have the server's code, message, and details.
// Other errors, such as a failed connection, wrap the cause.
type Error struct {
	// Procedure is the procedure that failed, such as
	// "/acme.v1.UserService/GetUser". Error does not include it.
	Procedure string
	Code      Code
	Message   string
	Details   []*ErrorDetail
	cause     error
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) Unwrap() error {
	return e.cause
}

// ErrorDetail is a Protobuf message that the server attached to an error.
type ErrorDetail struct {
	// Type is the full name of the message, such as "google.rpc.RetryInfo".
	Type string
	// Value is the message in binary Protobuf.
	Value []byte
}

// Message unmarshals the detail into a new message of its type. The type must
// be linked into the program.
func (d *ErrorDetail) Message() (proto.Message, error) {
	messageType, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(d.Type))
	if err != nil {
		return nil, fmt.Errorf("find error detail type %s: %w", d.Type, err)
	}
	message := messageType.New().Interface()
	if err := proto.Unmarshal(d.Value, message); err != nil {
		return nil, fmt.Errorf("unmarshal error detail %s: %w", d.Type, err)
	}
	return message, nil
}

// generatorVersion is the release of protoc-gen-connectclient-go that
// generated this code, or "devel". It is part of the default User-Agent.
const generatorVersion = "devel"

const (
	defaultReadMaxBytes = 4 << 20       // 4 MiB
	maxTimeoutMs        = 9_999_999_999 // the protocol allows at most 10 digits
	contentTypeProto    = "application/proto"
	contentTypeJSON     = "application/json"
)

// protocolHeaders are set by the client. WithHeader and hooks cannot change
// them.
var protocolHeaders = map[string]struct{}{
	"Accept-Encoding":          {},
	"Connect-Protocol-Version": {},
	"Connect-Timeout-Ms":       {},
	"Content-Encoding":         {},
	"Content-Length":           {},
	"Content-Type":             {},
}

type clientConfig struct {
	httpClient   HTTPClient
	header       http.Header
	readMaxBytes int64
	timeout      time.Duration
	hooks        []func(*http.Request) (func(*http.Response, error), error)
}

type optionFunc func(*clientConfig)

func (f optionFunc) applyToConfig(config *clientConfig) { f(config) }

// conn sends calls to one server. Service clients share it.
type conn struct {
	baseURL string
	config  *clientConfig
}

func newConn(baseURL string, options []Option) *conn {
	config := &clientConfig{
		httpClient:   http.DefaultClient,
		header:       make(http.Header),
		readMaxBytes: defaultReadMaxBytes,
	}
	for _, option := range options {
		option.applyToConfig(config)
	}
	for key := range protocolHeaders {
		config.header.Del(key)
	}
	if config.header.Get("User-Agent") == "" {
		config.header.Set("User-Agent", "connectclient-go/"+generatorVersion+" ("+runtime.Version()+")")
	}
	return &conn{baseURL: strings.TrimSuffix(baseURL, "/"), config: config}
}

// call sends req to procedure, for example "/acme.v1.UserService/GetUser",
// and unmarshals the response into res. It runs the WithHook hooks around the
// HTTP request.
func (c *conn) call(ctx context.Context, procedure string, req, res proto.Message) error {
	if _, ok := ctx.Deadline(); !ok && c.config.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.timeout)
		defer cancel()
	}
	body, err := proto.Marshal(req)
	if err != nil {
		return &Error{Procedure: procedure, Code: CodeInternal, Message: "marshal request: " + err.Error(), cause: err}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+procedure, http.NoBody)
	if err != nil {
		return &Error{Procedure: procedure, Code: CodeInternal, Message: "create request: " + err.Error(), cause: err}
	}
	for key, values := range c.config.header {
		// Copy the values, so code that changes this request cannot change
		// other requests.
		httpRequest.Header[key] = slices.Clone(values)
	}
	setBody(httpRequest, body)
	var dones []func(*http.Response, error)
	for _, hook := range c.config.hooks {
		done, err := hook(httpRequest)
		if err != nil {
			runDone(dones, nil, err)
			return err
		}
		if done != nil {
			dones = append(dones, done)
		}
	}
	httpResponse, err := c.send(ctx, httpRequest, body, res)
	if callError, ok := errors.AsType[*Error](err); ok {
		callError.Procedure = procedure
	}
	if httpResponse != nil {
		// The body is already read and closed.
		httpResponse.Body = http.NoBody
	}
	runDone(dones, httpResponse, err)
	return err
}

// runDone runs the done functions of hooks in reverse order.
func runDone(dones []func(*http.Response, error), httpResponse *http.Response, err error) {
	for index := len(dones) - 1; index >= 0; index-- {
		dones[index](httpResponse, err)
	}
}

// setBody sets body as the request body, again if a hook read or replaced it.
func setBody(httpRequest *http.Request, body []byte) {
	httpRequest.ContentLength = int64(len(body))
	if len(body) == 0 {
		// An empty message has no bytes. net/http treats a zero length with a
		// body other than http.NoBody as unknown, and sends it chunked.
		httpRequest.Body = http.NoBody
		httpRequest.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
		return
	}
	httpRequest.Body = io.NopCloser(bytes.NewReader(body))
	httpRequest.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

// send sets the protocol headers, sends the request, and reads the response.
// It returns the response, if one arrived, and the call's error.
func (c *conn) send(ctx context.Context, httpRequest *http.Request, body []byte, res proto.Message) (*http.Response, error) {
	setBody(httpRequest, body)
	for key := range protocolHeaders {
		httpRequest.Header.Del(key)
	}
	httpRequest.Header.Set("Content-Type", contentTypeProto)
	httpRequest.Header.Set("Connect-Protocol-Version", "1")
	// The timeout is calculated after the hooks, so it counts the time they
	// took, for example to refresh a token.
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout <= 0 {
			return nil, transportError(ctx, context.DeadlineExceeded)
		}
		timeoutMs := (timeout + time.Millisecond - 1).Milliseconds() // round up
		if timeoutMs <= maxTimeoutMs {
			httpRequest.Header.Set("Connect-Timeout-Ms", strconv.FormatInt(timeoutMs, 10))
		}
	}
	httpResponse, err := c.config.httpClient.Do(httpRequest)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer func() { _ = httpResponse.Body.Close() }()
	contentType, _, _ := strings.Cut(httpResponse.Header.Get("Content-Type"), ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if httpResponse.StatusCode != http.StatusOK && contentType != contentTypeJSON {
		// Not a Connect error, for example an HTML page from a proxy. Do not
		// read the body, so a large page still gets a code from its status.
		return httpResponse, &Error{Code: codeFromHTTPStatus(httpResponse.StatusCode), Message: httpResponse.Status}
	}
	if encoding := httpResponse.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		// net/http decompresses gzip itself and removes this header.
		return httpResponse, &Error{Code: CodeInternal, Message: fmt.Sprintf("unsupported response compression %q", encoding)}
	}
	reader := io.Reader(httpResponse.Body)
	if c.config.readMaxBytes > 0 {
		reader = io.LimitReader(reader, c.config.readMaxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return httpResponse, transportError(ctx, err)
	}
	if c.config.readMaxBytes > 0 && int64(len(data)) > c.config.readMaxBytes {
		return httpResponse, &Error{
			Code:    CodeResourceExhausted,
			Message: fmt.Sprintf("response is larger than the %d byte limit", c.config.readMaxBytes),
		}
	}
	if httpResponse.StatusCode != http.StatusOK {
		return httpResponse, errorFromJSON(httpResponse, data)
	}
	if contentType != contentTypeProto {
		// A response that does not look like Connect gets CodeUnknown, and one
		// with another codec gets CodeInternal, as in connect-go.
		code := CodeInternal
		if !strings.HasPrefix(contentType, "application/") {
			code = CodeUnknown
		}
		return httpResponse, &Error{Code: code, Message: fmt.Sprintf("invalid response content-type %q", contentType)}
	}
	if err := proto.Unmarshal(data, res); err != nil {
		return httpResponse, &Error{Code: CodeInvalidArgument, Message: "unmarshal response: " + err.Error(), cause: err}
	}
	return httpResponse, nil
}

// transportError converts an error from the HTTP client into an *Error that
// wraps it. Context errors take precedence.
func transportError(ctx context.Context, err error) error {
	if urlError, ok := errors.AsType[*url.Error](err); ok {
		err = urlError.Err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	code := CodeUnavailable
	switch {
	case errors.Is(err, context.Canceled):
		code = CodeCanceled
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		code = CodeDeadlineExceeded
	}
	return &Error{Code: code, Message: err.Error(), cause: err}
}

// errorFromJSON converts a non-200 response with a JSON body. A body that is
// not a Connect error gets a code from the HTTP status.
func errorFromJSON(httpResponse *http.Response, data []byte) error {
	var wire struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"details"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return &Error{Code: codeFromHTTPStatus(httpResponse.StatusCode), Message: httpResponse.Status}
	}
	callError := &Error{Code: codeFromHTTPStatus(httpResponse.StatusCode), Message: wire.Message}
	if code := Code(wire.Code); code.valid() {
		callError.Code = code
	} else if callError.Message == "" {
		// JSON that is not a Connect error, for example {"error": "..."}
		// from a gateway.
		callError.Message = httpResponse.Status
	}
	for _, detail := range wire.Details {
		// The value is base64, with or without padding.
		value, err := base64Decode(detail.Value)
		if err != nil {
			continue // keep the code and message, as connect-go does
		}
		callError.Details = append(callError.Details, &ErrorDetail{Type: detail.Type, Value: value})
	}
	return callError
}

func base64Decode(value string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(value, "="))
}

func (c Code) valid() bool {
	switch c {
	case CodeCanceled, CodeUnknown, CodeInvalidArgument, CodeDeadlineExceeded, CodeNotFound,
		CodeAlreadyExists, CodePermissionDenied, CodeResourceExhausted, CodeFailedPrecondition,
		CodeAborted, CodeOutOfRange, CodeUnimplemented, CodeInternal, CodeUnavailable,
		CodeDataLoss, CodeUnauthenticated:
		return true
	default:
		return false
	}
}

// codeFromHTTPStatus maps an HTTP status to a code, as the Connect protocol
// specifies for responses without an error body.
func codeFromHTTPStatus(status int) Code {
	switch status {
	case http.StatusBadRequest:
		return CodeInternal
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodePermissionDenied
	case http.StatusNotFound:
		return CodeUnimplemented
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return CodeUnavailable
	default:
		return CodeUnknown
	}
}
