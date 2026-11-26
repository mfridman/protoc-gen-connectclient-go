package connectclient_test

import (
	"context"
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

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/mfridman/protoc-gen-connectclient-go/connectclient"
	apiv1 "github.com/mfridman/protoc-gen-connectclient-go/internal/testdata/gen/api/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/testdata/gen/api/v1/apiv1client"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/testdata/gen/api/v1/apiv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestCallUnary(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL))
	res, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "42"})
	requireNoError(t, err)
	requireEqual(t, res.GetName(), "user 42")
	echo, err := client.UserService.Echo(t.Context(), &apiv1.EchoMessage{Text: "hello"})
	requireNoError(t, err)
	requireEqual(t, echo.GetText(), "hello")
	owner, err := client.OwnerService.GetOwner(t.Context(), &apiv1.GetOwnerRequest{Id: "7"})
	requireNoError(t, err)
	requireEqual(t, owner.GetName(), "owner 7")
}

func TestConcurrentCalls(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(
		newServer(t).URL,
		connectclient.WithHeader("X-Client", "shared"),
	))
	var waitGroup sync.WaitGroup
	errs := make(chan error, 64)
	for index := range 64 {
		waitGroup.Go(func() {
			ctx, callInfo := connect.NewClientContext(t.Context())
			callInfo.RequestHeader().Set("X-Index", strconv.Itoa(index))
			id := strconv.Itoa(index)
			res, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: id})
			if err == nil && res.GetName() != "user "+id {
				err = fmt.Errorf("got %q for id %s", res.GetName(), id)
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

func TestStockGeneratedClient(t *testing.T) {
	t.Parallel()
	// Clients from the stock protoc-gen-connect-go v2 plugin also work.
	transport := connectclient.NewTransport(newServer(t).URL)
	client := apiv1connect.NewUserServiceClient(connect.NewClient(transport))
	res, err := client.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
	requireNoError(t, err)
	requireEqual(t, res.GetName(), "user 1")
}

func TestErrorFromHandler(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL))
	_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "missing"})
	connectError := requireConnectError(t, err, connect.CodeNotFound)
	requireEqual(t, connectError.Message(), "user missing not found")
	requireEqual(t, connectError.IsRemote(), true)
	details := connectError.Details()
	requireEqual(t, len(details), 1)
	detail, err := connectproto.UnmarshalErrorDetail(details[0])
	requireNoError(t, err)
	retryDelay, ok := detail.(*durationpb.Duration)
	if !ok {
		t.Fatalf("detail is %T, want *durationpb.Duration", detail)
	}
	requireEqual(t, retryDelay.AsDuration(), 3*time.Second)
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
		wantCode    connect.Code
		wantMessage string
	}{
		{name: "bad_gateway", status: http.StatusBadGateway, contentType: "text/plain", body: "upstream down", wantCode: connect.CodeUnavailable, wantMessage: "502 Bad Gateway"},
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: connect.CodeUnauthenticated},
		{name: "forbidden", status: http.StatusForbidden, wantCode: connect.CodePermissionDenied},
		{name: "not_found", status: http.StatusNotFound, wantCode: connect.CodeUnimplemented},
		{name: "too_many_requests", status: http.StatusTooManyRequests, wantCode: connect.CodeUnavailable},
		{name: "bad_request", status: http.StatusBadRequest, wantCode: connect.CodeInternal},
		{name: "teapot", status: http.StatusTeapot, wantCode: connect.CodeUnknown},
		{name: "invalid_json", status: http.StatusInternalServerError, contentType: "application/json", body: "{", wantCode: connect.CodeUnknown, wantMessage: "500 Internal Server Error"},
		{name: "unknown_code", status: http.StatusServiceUnavailable, contentType: "application/json", body: `{"code":"bogus","message":"m"}`, wantCode: connect.CodeUnavailable, wantMessage: "m"},
		{name: "json_without_code", status: http.StatusForbidden, contentType: "application/json", body: `{"message":"m"}`, wantCode: connect.CodePermissionDenied, wantMessage: "m"},
		{name: "bad_detail", status: http.StatusConflict, contentType: "application/json", body: `{"code":"aborted","message":"m","details":[{"type":"x","value":"!!!"}]}`, wantCode: connect.CodeAborted, wantMessage: "m"},
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
			client := apiv1client.NewClient(connectclient.New(server.URL))
			_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
			connectError := requireConnectError(t, err, test.wantCode)
			if test.wantMessage != "" {
				requireEqual(t, connectError.Message(), test.wantMessage)
			}
			requireEqual(t, len(connectError.Details()), 0)
		})
	}
}

func TestLargeProxyError(t *testing.T) {
	t.Parallel()
	// A large non-Connect error page gets a code from its status, even when
	// it is larger than the read limit or compressed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "br")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, strings.Repeat("<p>down</p>", 1024))
	}))
	t.Cleanup(server.Close)
	client := apiv1client.NewClient(connectclient.New(server.URL, connectclient.WithReadMaxBytes(1024)))
	_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
	requireCode(t, err, connect.CodeUnavailable)
}

func TestUnimplemented(t *testing.T) {
	t.Parallel()
	// The server has no route for this procedure, so it answers 404.
	server := newServer(t)
	transport := connectclient.NewTransport(server.URL)
	stream, err := transport.NewClientStream(t.Context(), connect.Spec{
		StreamType: connect.StreamTypeUnary,
		Procedure:  "/api.v1.MissingService/Missing",
	})
	requireNoError(t, err)
	requireNoError(t, stream.Send(&apiv1.GetUserRequest{}))
	requireCode(t, stream.Receive(&apiv1.GetUserResponse{}), connect.CodeUnimplemented)
}

func TestStreamingRejected(t *testing.T) {
	t.Parallel()
	transport := connectclient.NewTransport(newServer(t).URL)
	for _, streamType := range []connect.StreamType{connect.StreamTypeClient, connect.StreamTypeServer, connect.StreamTypeBidi} {
		_, err := transport.NewClientStream(t.Context(), connect.Spec{StreamType: streamType, Procedure: "/a.B/C"})
		requireCode(t, err, connect.CodeUnimplemented)
	}
	// A streaming method on a stock generated client fails the same way.
	client := apiv1connect.NewRepositoryServiceClient(connect.NewClient(transport))
	_, err := client.WatchRepository(t.Context(), &apiv1.WatchRepositoryRequest{Name: "x"})
	requireCode(t, err, connect.CodeUnimplemented)
}

func TestDeadline(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	res, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "deadline"})
	requireNoError(t, err)
	requireEqual(t, res.GetName(), "5s")

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "slow"})
	requireCode(t, err, connect.CodeDeadlineExceeded)
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
	client := apiv1client.NewClient(connectclient.New(server.URL))
	_, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "1"})
	requireCode(t, err, connect.CodeDeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(%v, context.DeadlineExceeded) = false", err)
	}
	requireEqual(t, requests.Load(), int32(0))
}

func TestCanceled(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL))
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "slow"})
	requireCode(t, err, connect.CodeCanceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(%v, context.Canceled) = false", err)
	}
}

func TestUnavailable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	client := apiv1client.NewClient(connectclient.New(url))
	_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
	requireCode(t, err, connect.CodeUnavailable)
}

func TestHeaders(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL))
	ctx, callInfo := connect.NewClientContext(t.Context())
	callInfo.RequestHeader().Set("X-Client", "connectclient")
	// The transport owns protocol headers, so these do not reach the server.
	callInfo.RequestHeader().Set("Content-Type", "application/json")
	callInfo.RequestHeader().Set("Connect-Timeout-Ms", "1")
	res, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	requireEqual(t, res.GetName(), "X-Client=connectclient Content-Type=application/proto Connect-Timeout-Ms= User-Agent=connectclient-go")
	requireEqual(t, callInfo.ResponseHeader().Get("X-Server"), "yes")
	requireEqual(t, callInfo.ResponseTrailer().Get("X-Trailer"), "done")
	requireEqual(t, callInfo.ResponseHeader().Get("Trailer-X-Trailer"), "")
	requireEqual(t, callInfo.Protocol, connect.ProtocolNameConnect)
	requireEqual(t, callInfo.Codec, connect.CodecNameProto)
	requireEqual(t, callInfo.Spec.Procedure, apiv1client.UserServiceGetUserProcedure)
	if callInfo.PeerAddr == "" {
		t.Fatal("PeerAddr is empty")
	}
	if callInfo.SendStats.Size == 0 || callInfo.ReceiveStats.Size == 0 {
		t.Fatalf("message stats not set: send=%+v receive=%+v", callInfo.SendStats, callInfo.ReceiveStats)
	}
}

func TestUserAgent(t *testing.T) {
	t.Parallel()
	server := newServer(t)
	client := apiv1client.NewClient(connectclient.New(server.URL, connectclient.WithHeader("User-Agent", "my-cli/1.0")))
	res, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	if !strings.HasSuffix(res.GetName(), "User-Agent=my-cli/1.0") {
		t.Fatalf("got %q", res.GetName())
	}
	ctx, callInfo := connect.NewClientContext(t.Context())
	callInfo.RequestHeader().Set("User-Agent", "per-call")
	res, err = client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	if !strings.HasSuffix(res.GetName(), "User-Agent=per-call") {
		t.Fatalf("got %q", res.GetName())
	}
}

func TestWithHeader(t *testing.T) {
	t.Parallel()
	server := newServer(t)
	client := apiv1client.NewClient(connectclient.New(
		server.URL,
		connectclient.WithHeader("X-Client", "option"),
		connectclient.WithHeader("Content-Type", "application/json"),
	))
	res, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	requireEqual(t, res.GetName(), "X-Client=option Content-Type=application/proto Connect-Timeout-Ms= User-Agent=connectclient-go")
	// A header on the CallInfo replaces the option for that call.
	ctx, callInfo := connect.NewClientContext(t.Context())
	callInfo.RequestHeader().Set("X-Client", "call")
	res, err = client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	if !strings.HasPrefix(res.GetName(), "X-Client=call ") {
		t.Fatalf("got %q", res.GetName())
	}
}

func TestCallInfoReuse(t *testing.T) {
	t.Parallel()
	// A reused client context gets fresh response metadata for each call.
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL))
	ctx, callInfo := connect.NewClientContext(t.Context())
	_, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	requireEqual(t, callInfo.ResponseHeader().Get("X-Server"), "yes")
	_, err = client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "1"})
	requireNoError(t, err)
	requireEqual(t, callInfo.ResponseHeader().Get("X-Server"), "")
	requireEqual(t, callInfo.ResponseTrailer().Get("X-Trailer"), "")
}

func TestReadMaxBytes(t *testing.T) {
	t.Parallel()
	server := newServer(t)
	request := &apiv1.GetUserRequest{Id: "large"} // a 64 KiB name

	client := apiv1client.NewClient(connectclient.New(server.URL, connectclient.WithReadMaxBytes(1024)))
	_, err := client.UserService.GetUser(t.Context(), request)
	requireCode(t, err, connect.CodeResourceExhausted)

	client = apiv1client.NewClient(connectclient.New(server.URL, connectclient.WithReadMaxBytes(0)))
	res, err := client.UserService.GetUser(t.Context(), request)
	requireNoError(t, err)
	requireEqual(t, len(res.GetName()), 64<<10)
}

func TestGzipResponse(t *testing.T) {
	t.Parallel()
	// net/http asks for gzip and decompresses, and connecthttp compresses.
	server := newServer(t, connecthttp.WithCompressMinBytes(1024))
	client := apiv1client.NewClient(connectclient.New(server.URL))
	ctx, callInfo := connect.NewClientContext(t.Context())
	res, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "large"})
	requireNoError(t, err)
	requireEqual(t, len(res.GetName()), 64<<10)
	requireEqual(t, callInfo.ResponseEncoding, connect.CompressionNameGzip)
	// The limit applies to the decompressed size.
	client = apiv1client.NewClient(connectclient.New(server.URL, connectclient.WithReadMaxBytes(32<<10)))
	_, err = client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "large"})
	requireCode(t, err, connect.CodeResourceExhausted)
}

func TestUnexpectedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantCode connect.Code
	}{
		{
			name: "not_connect_content_type",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, "<html></html>")
			},
			wantCode: connect.CodeUnknown,
		},
		{
			name: "other_codec",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, "{}")
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "compression",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/proto")
				w.Header().Set("Content-Encoding", "br")
				_, _ = io.WriteString(w, "data")
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "invalid_protobuf",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/proto")
				_, _ = w.Write([]byte{0xff, 0xff, 0xff})
			},
			wantCode: connect.CodeInvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(test.handler)
			t.Cleanup(server.Close)
			client := apiv1client.NewClient(connectclient.New(server.URL))
			_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
			requireCode(t, err, test.wantCode)
		})
	}
}

func TestInterceptors(t *testing.T) {
	t.Parallel()
	var gotSpec connect.Spec
	var gotErr error
	interceptor := func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			gotSpec = spec
			if callInfo, ok := connect.CallInfoForClientContext(ctx); ok {
				callInfo.RequestHeader().Set("X-Client", "interceptor")
			}
			stream, err := next(ctx, spec)
			if err != nil {
				return nil, err
			}
			return &observedStream{ClientStream: stream, onError: func(err error) { gotErr = err }}, nil
		}
	}
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL, connectclient.WithInterceptors(interceptor)))
	res, err := client.RepositoryService.GetRepository(t.Context(), &apiv1.GetRepositoryRequest{Name: "headers"})
	requireNoError(t, err)
	requireEqual(t, res.GetRepository().GetDescription(), "interceptor")
	// Generated specs carry the method descriptor and idempotency level.
	requireEqual(t, gotSpec.IdempotencyLevel, connect.IdempotencyNoSideEffects)
	methodDescriptor, ok := gotSpec.Schema.(protoreflect.MethodDescriptor)
	if !ok {
		t.Fatalf("Schema is %T, want protoreflect.MethodDescriptor", gotSpec.Schema)
	}
	requireEqual(t, methodDescriptor.FullName(), protoreflect.FullName("api.v1.RepositoryService.GetRepository"))
	methodOptions, ok := methodDescriptor.Options().(*descriptorpb.MethodOptions)
	if !ok {
		t.Fatalf("method options are %T", methodDescriptor.Options())
	}
	requireEqual(t, methodOptions.GetIdempotencyLevel(), descriptorpb.MethodOptions_NO_SIDE_EFFECTS)

	_, err = client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "missing"})
	requireCode(t, err, connect.CodeNotFound)
	requireCode(t, gotErr, connect.CodeNotFound)
}

func TestUnixSocket(t *testing.T) {
	t.Parallel()
	socketPath := filepath.Join(shortTempDir(t), "api.sock")
	listener, err := net.Listen("unix", socketPath)
	requireNoError(t, err)
	server := httptest.NewUnstartedServer(newHandler())
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	httpClient := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}}
	client := apiv1client.NewClient(connectclient.New("http://unix", connectclient.WithHTTPClient(httpClient)))
	res, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "unix"})
	requireNoError(t, err)
	requireEqual(t, res.GetName(), "user unix")
}

func TestUnixSocketErrorsKeepCause(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix error numbers")
	}
	// Callers classify failures, for example "daemon not running", from the
	// cause of the *connect.Error.
	unixClient := func(socketPath string, timeout time.Duration) *apiv1client.Client {
		httpClient := &http.Client{Timeout: timeout, Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		}}
		return apiv1client.NewClient(connectclient.New("http://unix", connectclient.WithHTTPClient(httpClient)))
	}
	dir := shortTempDir(t)

	_, err := unixClient(filepath.Join(dir, "missing.sock"), 0).UserService.GetUser(t.Context(), &apiv1.GetUserRequest{})
	requireCode(t, err, connect.CodeUnavailable)
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
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections <- conn
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		for conn := range connections {
			_ = conn.Close()
		}
	})
	_, err = unixClient(hungPath, 100*time.Millisecond).UserService.GetUser(t.Context(), &apiv1.GetUserRequest{})
	requireCode(t, err, connect.CodeDeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung server: errors.Is(%v, context.DeadlineExceeded) = false", err)
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New(newServer(t).URL + "/"))
	_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
	requireNoError(t, err)
}

func TestInvalidBaseURL(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(connectclient.New("http://[::1"))
	_, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "1"})
	requireCode(t, err, connect.CodeInternal)
}

func TestSendNonProtoMessage(t *testing.T) {
	t.Parallel()
	stream, err := connectclient.NewTransport("http://localhost").NewClientStream(t.Context(), connect.Spec{Procedure: "/a.B/C"})
	requireNoError(t, err)
	requireCode(t, stream.Send("not a message"), connect.CodeInternal)
}

type observedStream struct {
	connect.ClientStream
	onError func(error)
}

func (s *observedStream) Receive(message any) error {
	err := s.ClientStream.Receive(message)
	if err != nil && !errors.Is(err, io.EOF) {
		s.onError(err)
	}
	return err
}

func newServer(t *testing.T, options ...connecthttp.Option) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(newHandler(options...))
	t.Cleanup(server.Close)
	return server
}

func newHandler(options ...connecthttp.Option) http.Handler {
	server := connect.NewServer()
	apiv1connect.RegisterUserServiceHandler(server, &userService{})
	apiv1connect.RegisterOwnerServiceHandler(server, &ownerService{})
	apiv1connect.RegisterRepositoryServiceHandler(server, &repositoryService{})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, options...)
	return mux
}

type userService struct {
	apiv1connect.UnimplementedUserServiceHandler
}

func (*userService) GetUser(ctx context.Context, req *apiv1.GetUserRequest) (*apiv1.GetUserResponse, error) {
	callInfo, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, "no call info")
	}
	switch req.GetId() {
	case "missing":
		detail, err := connectproto.NewErrorDetail(durationpb.New(3 * time.Second))
		if err != nil {
			return nil, err
		}
		return nil, connect.NewError(connect.CodeNotFound, "user missing not found").WithDetail(detail)
	case "deadline":
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, "no deadline")
		}
		return &apiv1.GetUserResponse{Name: time.Until(deadline).Round(time.Second).String()}, nil
	case "slow":
		<-ctx.Done()
		return nil, ctx.Err()
	case "large":
		return &apiv1.GetUserResponse{Name: strings.Repeat("a", 64<<10)}, nil
	case "headers":
		callInfo.ResponseHeader().Set("X-Server", "yes")
		callInfo.ResponseTrailer().Set("X-Trailer", "done")
		requestHeader := callInfo.RequestHeader()
		var pairs []string
		for _, key := range []string{"X-Client", "Content-Type", "Connect-Timeout-Ms", "User-Agent"} {
			pairs = append(pairs, key+"="+requestHeader.Get(key))
		}
		return &apiv1.GetUserResponse{Name: strings.Join(pairs, " ")}, nil
	}
	return &apiv1.GetUserResponse{Name: "user " + req.GetId()}, nil
}

func (*userService) Echo(_ context.Context, req *apiv1.EchoMessage) (*apiv1.EchoMessage, error) {
	return proto.CloneOf(req), nil
}

type ownerService struct {
	apiv1connect.UnimplementedOwnerServiceHandler
}

func (*ownerService) GetOwner(_ context.Context, req *apiv1.GetOwnerRequest) (*apiv1.GetOwnerResponse, error) {
	return &apiv1.GetOwnerResponse{Name: "owner " + req.GetId()}, nil
}

type repositoryService struct {
	apiv1connect.UnimplementedRepositoryServiceHandler
}

func (*repositoryService) GetRepository(ctx context.Context, req *apiv1.GetRepositoryRequest) (*apiv1.GetRepositoryResponse, error) {
	callInfo, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, "no call info")
	}
	return &apiv1.GetRepositoryResponse{Repository: &apiv1.Repository{
		Name:        req.GetName(),
		Description: callInfo.RequestHeader().Get("X-Client"),
	}}, nil
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

func requireCode(t *testing.T, err error, wantCode connect.Code) {
	t.Helper()
	_ = requireConnectError(t, err, wantCode)
}

func requireConnectError(t *testing.T, err error, wantCode connect.Code) *connect.Error {
	t.Helper()
	connectError, ok := errors.AsType[*connect.Error](err)
	if !ok {
		t.Fatalf("error %v (%T) is not a *connect.Error", err, err)
	}
	if connectError.Code() != wantCode {
		t.Fatalf("code = %v, want %v (error: %v)", connectError.Code(), wantCode, err)
	}
	return connectError
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
