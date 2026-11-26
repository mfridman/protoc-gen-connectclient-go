package connectclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"
)

const (
	contentTypeProto = "application/proto"
	contentTypeJSON  = "application/json"

	headerContentType     = "Content-Type"
	headerProtocolVersion = "Connect-Protocol-Version"
	headerTimeout         = "Connect-Timeout-Ms"
	headerUserAgent       = "User-Agent"
	trailerPrefix         = "Trailer-"

	protocolVersion = "1"
	maxTimeoutMs    = 9_999_999_999 // the protocol allows at most 10 digits
)

// protocolHeaders are set by the transport. Request headers on the
// connect.CallInfo cannot override them.
var protocolHeaders = map[string]struct{}{
	"Accept-Encoding":     {},
	"Content-Encoding":    {},
	"Content-Length":      {},
	headerContentType:     {},
	headerProtocolVersion: {},
	headerTimeout:         {},
}

// unaryStream is the connect.ClientStream for one unary RPC. Send stores the
// request, and the first Receive sends it and reads the response.
type unaryStream struct {
	ctx       context.Context
	transport *Transport
	spec      connect.Spec
	callInfo  *connect.CallInfo
	request   []byte
	response  []byte
	err       error
	called    bool
	received  bool
}

func (s *unaryStream) SendHeaders() error { return nil }

func (s *unaryStream) Send(message any) error {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return connect.Errorf(connect.CodeInternal, "connectclient: %T is not a proto.Message", message)
	}
	data, err := proto.Marshal(protoMessage)
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "marshal request: %v", err)
	}
	s.request = data
	if s.callInfo != nil {
		s.callInfo.SendStats = connect.MessageStats{Size: len(data)}
	}
	return nil
}

func (s *unaryStream) CloseSend() error { return nil }

func (s *unaryStream) Receive(message any) error {
	if s.received {
		return io.EOF
	}
	if !s.called {
		s.called = true
		s.err = s.call()
	}
	if s.err != nil {
		return s.err
	}
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return connect.Errorf(connect.CodeInternal, "connectclient: %T is not a proto.Message", message)
	}
	s.received = true
	if err := proto.Unmarshal(s.response, protoMessage); err != nil {
		// connecthttp uses the same code.
		return connect.Errorf(connect.CodeInvalidArgument, "unmarshal response: %v", err).WithCause(err)
	}
	return nil
}

func (s *unaryStream) Close() error { return nil }

func (s *unaryStream) call() error {
	httpRequest, err := http.NewRequestWithContext(
		s.ctx,
		http.MethodPost,
		s.transport.baseURL+s.spec.Procedure,
		bytes.NewReader(s.request),
	)
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "create request: %v", err)
	}
	httpRequest.Header.Set(headerUserAgent, s.transport.userAgent)
	// Copy the values, so that code that changes the request headers cannot
	// change the headers of other calls.
	for key, values := range s.transport.header {
		if _, reserved := protocolHeaders[key]; !reserved {
			httpRequest.Header[key] = slices.Clone(values)
		}
	}
	if s.callInfo != nil {
		s.callInfo.PeerAddr = httpRequest.URL.Host
		for key, values := range s.callInfo.RequestHeader().All() {
			if _, reserved := protocolHeaders[key]; !reserved {
				httpRequest.Header[key] = slices.Clone(values)
			}
		}
	}
	httpRequest.Header.Set(headerContentType, contentTypeProto)
	httpRequest.Header.Set(headerProtocolVersion, protocolVersion)
	if deadline, ok := s.ctx.Deadline(); ok {
		timeoutMs := ceilMilliseconds(time.Until(deadline))
		if timeoutMs <= 0 {
			return s.transportError(context.DeadlineExceeded)
		}
		if timeoutMs <= maxTimeoutMs {
			httpRequest.Header.Set(headerTimeout, strconv.FormatInt(timeoutMs, 10))
		}
	}
	httpResponse, err := s.transport.httpClient.Do(httpRequest)
	if err != nil {
		return s.transportError(err)
	}
	defer func() { _ = httpResponse.Body.Close() }()
	s.recordResponseHeaders(httpResponse)
	contentType := mediaType(httpResponse.Header.Get(headerContentType))
	if httpResponse.StatusCode != http.StatusOK && contentType != contentTypeJSON {
		// Not a Connect error, for example an HTML page from a proxy. Do not
		// read the body, so a large page still gets a code from its status.
		return connect.NewError(codeFromHTTPStatus(httpResponse.StatusCode), httpResponse.Status).WithRemote()
	}
	if encoding := httpResponse.Header.Get("Content-Encoding"); encoding != "" && encoding != connect.CompressionNameIdentity {
		return connect.Errorf(connect.CodeInternal, "unsupported response compression %q", encoding)
	}
	data, err := s.readBody(httpResponse.Body)
	if err != nil {
		return err
	}
	if httpResponse.StatusCode != http.StatusOK {
		return errorFromResponse(httpResponse, data)
	}
	if contentType != contentTypeProto {
		// A response that does not look like Connect at all gets CodeUnknown,
		// and one with another codec gets CodeInternal, as in connecthttp.
		code := connect.CodeInternal
		if !strings.HasPrefix(contentType, "application/") {
			code = connect.CodeUnknown
		}
		return connect.Errorf(code, "invalid response content-type %q, want %q", contentType, contentTypeProto)
	}
	if s.callInfo != nil {
		s.callInfo.ReceiveStats = connect.MessageStats{Size: len(data)}
	}
	s.response = data
	return nil
}

func (s *unaryStream) readBody(body io.Reader) ([]byte, error) {
	readMaxBytes := s.transport.readMaxBytes
	if readMaxBytes > 0 {
		body = io.LimitReader(body, readMaxBytes+1)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, s.transportError(err)
	}
	if readMaxBytes > 0 && int64(len(data)) > readMaxBytes {
		return nil, connect.Errorf(connect.CodeResourceExhausted, "response is larger than the %d byte limit", readMaxBytes)
	}
	return data, nil
}

// recordResponseHeaders copies response headers to the connect.CallInfo. The
// Connect protocol sends unary trailers as headers with a "Trailer-" prefix.
func (s *unaryStream) recordResponseHeaders(httpResponse *http.Response) {
	if s.callInfo == nil {
		return
	}
	if httpResponse.Uncompressed {
		// net/http asked for gzip and decompressed the body.
		s.callInfo.ResponseEncoding = connect.CompressionNameGzip
	}
	for key, values := range httpResponse.Header {
		if name, ok := strings.CutPrefix(key, trailerPrefix); ok {
			s.callInfo.ResponseTrailer().SetValues(name, values)
			continue
		}
		s.callInfo.ResponseHeader().SetValues(key, values)
	}
}

// transportError converts an error from the HTTP client into a
// *connect.Error. Context errors take precedence, so a canceled or expired
// call reports CodeCanceled or CodeDeadlineExceeded.
func (s *unaryStream) transportError(err error) error {
	if urlError, ok := errors.AsType[*url.Error](err); ok {
		err = urlError.Err
	}
	if ctxErr := s.ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	switch {
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err.Error()).WithCause(err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, err.Error()).WithCause(err)
	default:
		return connect.NewError(connect.CodeUnavailable, err.Error()).WithCause(err)
	}
}

// mediaType returns the lowercase media type of a Content-Type header, without
// parameters such as charset.
func mediaType(contentType string) string {
	contentType, _, _ = strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(contentType))
}

func ceilMilliseconds(duration time.Duration) int64 {
	milliseconds := duration.Milliseconds()
	if duration%time.Millisecond > 0 {
		milliseconds++
	}
	return milliseconds
}
