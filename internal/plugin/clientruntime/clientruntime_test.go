package clientruntime

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const testProcedure = "/test.v1.TestService/Echo"

func TestCall(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != testProcedure {
			http.Error(w, "bad request line", http.StatusTeapot)
			return
		}
		for name, want := range map[string]string{
			"Content-Type":             "application/proto",
			"Connect-Protocol-Version": "1",
			"User-Agent":               "connectclient-go",
		} {
			if got := r.Header.Get(name); got != want {
				http.Error(w, name+"="+got, http.StatusTeapot)
				return
			}
		}
		writeProto(w, wrapperspb.String("echo "+readString(r)))
	}))
	t.Cleanup(server.Close)
	got := callString(t, newConn(server.URL+"/", nil), t.Context(), "hello")
	requireEqual(t, got, "echo hello")
}

func TestHeaders(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var pairs []string
		for _, name := range []string{"Authorization", "User-Agent", "Content-Type", "Connect-Timeout-Ms"} {
			pairs = append(pairs, name+"="+r.Header.Get(name))
		}
		writeProto(w, wrapperspb.String(strings.Join(pairs, " ")))
	}))
	t.Cleanup(server.Close)
	conn := newConn(server.URL, []Option{
		WithHeader("Authorization", "Bearer token"),
		WithHeader("User-Agent", "my-cli/1.0"),
		WithHeader("Content-Type", "application/json"),
		WithHeader("Connect-Timeout-Ms", "1"),
	})
	got := callString(t, conn, t.Context(), "")
	requireEqual(t, got, "Authorization=Bearer token User-Agent=my-cli/1.0 Content-Type=application/proto Connect-Timeout-Ms=")
}

func TestDeadline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProto(w, wrapperspb.String(r.Header.Get("Connect-Timeout-Ms")))
	}))
	t.Cleanup(server.Close)
	conn := newConn(server.URL, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	timeoutMs, err := strconv.Atoi(callString(t, conn, ctx, ""))
	requireNoError(t, err)
	if timeoutMs < 4000 || timeoutMs > 5000 {
		t.Fatalf("Connect-Timeout-Ms = %d, want about 5000", timeoutMs)
	}
}

func TestExpiredDeadlineSendsNothing(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	err := newConn(server.URL, nil).call(ctx, testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeDeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(%v, context.DeadlineExceeded) = false", err)
	}
	requireEqual(t, requests.Load(), int32(0))
}

func TestErrorDetails(t *testing.T) {
	t.Parallel()
	detail, err := proto.Marshal(durationpb.New(3 * time.Second))
	requireNoError(t, err)
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		body := fmt.Sprintf(
			`{"code":"not_found","message":"no user","details":[{"type":"google.protobuf.Duration","value":%q,"debug":{"seconds":"3"}}]}`,
			encoding.EncodeToString(detail),
		)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(server.Close)
		err := newConn(server.URL, nil).call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
		callError := requireCode(t, err, CodeNotFound)
		requireEqual(t, callError.Message, "no user")
		requireEqual(t, err.Error(), "not_found: no user")
		requireEqual(t, len(callError.Details), 1)
		message, err := callError.Details[0].Message()
		requireNoError(t, err)
		duration, ok := message.(*durationpb.Duration)
		if !ok {
			t.Fatalf("detail is %T, want *durationpb.Duration", message)
		}
		requireEqual(t, duration.AsDuration(), 3*time.Second)
	}
}

func TestErrorFromHTTPStatus(t *testing.T) {
	t.Parallel()
	// Responses without a Connect error body, for example from a proxy, get a
	// code from the HTTP status.
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantCode    Code
		wantMessage string
	}{
		{name: "bad_gateway", status: http.StatusBadGateway, contentType: "text/plain", body: "down", wantCode: CodeUnavailable, wantMessage: "502 Bad Gateway"},
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: CodeUnauthenticated},
		{name: "forbidden", status: http.StatusForbidden, wantCode: CodePermissionDenied},
		{name: "not_found", status: http.StatusNotFound, wantCode: CodeUnimplemented},
		{name: "too_many_requests", status: http.StatusTooManyRequests, wantCode: CodeUnavailable},
		{name: "bad_request", status: http.StatusBadRequest, wantCode: CodeInternal},
		{name: "teapot", status: http.StatusTeapot, wantCode: CodeUnknown},
		{name: "invalid_json", status: http.StatusInternalServerError, contentType: "application/json", body: "{", wantCode: CodeUnknown, wantMessage: "500 Internal Server Error"},
		{name: "unknown_code", status: http.StatusServiceUnavailable, contentType: "application/json", body: `{"code":"bogus","message":"m"}`, wantCode: CodeUnavailable, wantMessage: "m"},
		{name: "json_charset", status: http.StatusConflict, contentType: "application/json; charset=utf-8", body: `{"code":"aborted","message":"m"}`, wantCode: CodeAborted, wantMessage: "m"},
		{name: "bad_detail", status: http.StatusConflict, contentType: "application/json", body: `{"code":"aborted","message":"m","details":[{"type":"x","value":"!!!"}]}`, wantCode: CodeAborted, wantMessage: "m"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.contentType != "" {
					w.Header().Set("Content-Type", test.contentType)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			err := newConn(server.URL, nil).call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
			callError := requireCode(t, err, test.wantCode)
			if test.wantMessage != "" {
				requireEqual(t, callError.Message, test.wantMessage)
			}
			requireEqual(t, len(callError.Details), 0)
		})
	}
}

func TestUnexpectedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		encoding    string
		body        []byte
		wantCode    Code
	}{
		{name: "not_connect_content_type", contentType: "text/html", body: []byte("<html></html>"), wantCode: CodeUnknown},
		{name: "other_codec", contentType: "application/json", body: []byte("{}"), wantCode: CodeInternal},
		{name: "compression", contentType: "application/proto", encoding: "br", body: []byte("data"), wantCode: CodeInternal},
		{name: "invalid_protobuf", contentType: "application/proto", body: []byte{0xff, 0xff, 0xff}, wantCode: CodeInvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				if test.encoding != "" {
					w.Header().Set("Content-Encoding", test.encoding)
				}
				_, _ = w.Write(test.body)
			}))
			t.Cleanup(server.Close)
			err := newConn(server.URL, nil).call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
			checkCode(t, err, test.wantCode)
		})
	}
}

func TestReadMaxBytes(t *testing.T) {
	t.Parallel()
	large := strings.Repeat("a", 64<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err := proto.Marshal(wrapperspb.String(large))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/proto")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			// net/http asks for gzip and decompresses the response.
			w.Header().Set("Content-Encoding", "gzip")
			gzipWriter := gzip.NewWriter(w)
			defer func() { _ = gzipWriter.Close() }()
			_, _ = gzipWriter.Write(response)
			return
		}
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)

	// The limit applies to the decompressed size.
	err := newConn(server.URL, []Option{WithReadMaxBytes(32 << 10)}).
		call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeResourceExhausted)

	for _, limit := range []int64{0, defaultReadMaxBytes} {
		got := callString(t, newConn(server.URL, []Option{WithReadMaxBytes(limit)}), t.Context(), "")
		requireEqual(t, len(got), len(large))
	}
}

func TestLargeProxyError(t *testing.T) {
	t.Parallel()
	// A large non-Connect error page gets a code from its status, even when it
	// is larger than the read limit or compressed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "br")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, strings.Repeat("<p>down</p>", 1024))
	}))
	t.Cleanup(server.Close)
	err := newConn(server.URL, []Option{WithReadMaxBytes(1024)}).
		call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeUnavailable)
}

func TestTransportErrors(t *testing.T) {
	t.Parallel()
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	err := newConn(slow.URL, nil).call(ctx, testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeCanceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(%v, context.Canceled) = false", err)
	}

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err = newConn(slow.URL, nil).call(ctx, testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeDeadlineExceeded)

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	err = newConn(closedURL, nil).call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeUnavailable)

	err = newConn("http://[::1", nil).call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeInternal)
}

func TestUnixSocketErrorsKeepCause(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix error numbers")
	}
	// Callers classify failures, for example "daemon not running", from the
	// cause of the *Error.
	unixConn := func(socketPath string, timeout time.Duration) *conn {
		httpClient := &http.Client{Timeout: timeout, Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		}}
		return newConn("http://unix", []Option{WithHTTPClient(httpClient)})
	}
	dir := shortTempDir(t)

	err := unixConn(filepath.Join(dir, "missing.sock"), 0).
		call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeUnavailable)
	if _, ok := errors.AsType[*net.OpError](err); !ok || !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("missing socket: error %v does not wrap a *net.OpError with ENOENT", err)
	}

	// A server that accepts connections but never answers.
	hungPath := filepath.Join(dir, "hung.sock")
	listener, err := net.Listen("unix", hungPath)
	requireNoError(t, err)
	connections := make(chan net.Conn, 1)
	go func() {
		defer close(connections)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connections <- connection
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		for connection := range connections {
			_ = connection.Close()
		}
	})
	err = unixConn(hungPath, 100*time.Millisecond).
		call(t.Context(), testProcedure, wrapperspb.String(""), &wrapperspb.StringValue{})
	checkCode(t, err, CodeDeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung server: errors.Is(%v, context.DeadlineExceeded) = false", err)
	}
}

func TestConcurrentCalls(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProto(w, wrapperspb.String(r.Header.Get("X-Client")+" "+readString(r)))
	}))
	t.Cleanup(server.Close)
	conn := newConn(server.URL, []Option{WithHeader("X-Client", "shared")})
	var waitGroup sync.WaitGroup
	errs := make(chan error, 64)
	for index := range 64 {
		waitGroup.Go(func() {
			want := "shared " + strconv.Itoa(index)
			res := &wrapperspb.StringValue{}
			err := conn.call(t.Context(), testProcedure, wrapperspb.String(strconv.Itoa(index)), res)
			if err == nil && res.GetValue() != want {
				err = fmt.Errorf("got %q, want %q", res.GetValue(), want)
			}
			errs <- err
		})
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		requireNoError(t, err)
	}
}

func TestAfterCall(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if readString(r) == "fail" {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		writeProto(w, wrapperspb.String("ok"))
	}))
	t.Cleanup(server.Close)
	type contextKey struct{}
	var calls []string
	hook := func(ctx context.Context, err error) {
		requestID, _ := ctx.Value(contextKey{}).(string)
		calls = append(calls, fmt.Sprintf("%s %v", requestID, err))
	}
	conn := newConn(server.URL, []Option{WithAfterCall(hook), WithAfterCall(hook)})
	ctx := context.WithValue(t.Context(), contextKey{}, "request-1")
	requireNoError(t, conn.call(ctx, testProcedure, wrapperspb.String("hello"), &wrapperspb.StringValue{}))
	err := conn.call(ctx, testProcedure, wrapperspb.String("fail"), &wrapperspb.StringValue{})
	callError := requireCode(t, err, CodeUnavailable)
	requireEqual(t, callError.Procedure, testProcedure)
	requireEqual(t, err.Error(), "unavailable: 503 Service Unavailable")
	want := []string{
		"request-1 <nil>",
		"request-1 <nil>",
		"request-1 unavailable: 503 Service Unavailable",
		"request-1 unavailable: 503 Service Unavailable",
	}
	requireEqual(t, strings.Join(calls, "\n"), strings.Join(want, "\n"))
}

func TestCodeOf(t *testing.T) {
	t.Parallel()
	requireEqual(t, CodeOf(nil), Code(""))
	requireEqual(t, CodeOf(errors.New("other")), CodeUnknown)
	wrapped := fmt.Errorf("get user: %w", &Error{Code: CodeNotFound})
	requireEqual(t, CodeOf(wrapped), CodeNotFound)
	requireEqual(t, (&Error{Code: CodeInternal}).Error(), "internal")
}

func TestErrorDetailUnknownType(t *testing.T) {
	t.Parallel()
	_, err := (&ErrorDetail{Type: "example.v1.Missing"}).Message()
	if err == nil {
		t.Fatal("no error for an unknown detail type")
	}
}

func callString(t *testing.T, conn *conn, ctx context.Context, value string) string {
	t.Helper()
	res := &wrapperspb.StringValue{}
	requireNoError(t, conn.call(ctx, testProcedure, wrapperspb.String(value), res))
	return res.GetValue()
}

func readString(r *http.Request) string {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	req := &wrapperspb.StringValue{}
	if err := proto.Unmarshal(data, req); err != nil {
		return ""
	}
	return req.GetValue()
}

func writeProto(w http.ResponseWriter, message proto.Message) {
	data, err := proto.Marshal(message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/proto")
	_, _ = w.Write(data)
}

// shortTempDir returns a temporary directory with a short path, because Unix
// socket paths are limited to about 100 bytes.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cc")
	requireNoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func checkCode(t *testing.T, err error, wantCode Code) {
	t.Helper()
	_ = requireCode(t, err, wantCode)
}

func requireCode(t *testing.T, err error, wantCode Code) *Error {
	t.Helper()
	callError, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error %v (%T) is not an *Error", err, err)
	}
	if callError.Code != wantCode {
		t.Fatalf("code = %v, want %v (error: %v)", callError.Code, wantCode, err)
	}
	return callError
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
