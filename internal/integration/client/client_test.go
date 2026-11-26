// Package client tests generated clients against connect-go v2 servers.
package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/admin/v1/adminv1client"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/admin/v1/adminv1connect"
	apiv1 "github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1/apiv1client"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1/apiv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestCalls(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(newServer(t).URL)
	user, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "42"})
	requireNoError(t, err)
	requireEqual(t, user.GetName(), "user 42")
	echo, err := client.UserService.Echo(t.Context(), &apiv1.EchoMessage{Text: "hello"})
	requireNoError(t, err)
	requireEqual(t, echo.GetText(), "hello")
	owner, err := client.OwnerService.GetOwner(t.Context(), &apiv1.GetOwnerRequest{Id: "7"})
	requireNoError(t, err)
	requireEqual(t, owner.GetName(), "owner 7")
	repository, err := client.RepositoryService.GetRepository(t.Context(), &apiv1.GetRepositoryRequest{Name: "goose"})
	requireNoError(t, err)
	requireEqual(t, repository.GetRepository().GetName(), "goose")
	// A service from another package, with messages from google.protobuf.
	_, err = adminv1client.NewClient(newServer(t).URL).AdminService.Health(t.Context(), &emptypb.Empty{}) //nolint:staticcheck // the service is deprecated on purpose
	requireNoError(t, err)
}

func TestErrorDetails(t *testing.T) {
	t.Parallel()
	_, err := apiv1client.NewClient(newServer(t).URL).UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "missing"})
	callError := requireCode(t, err, apiv1client.CodeNotFound)
	requireEqual(t, callError.Message, "user missing not found")
	requireEqual(t, len(callError.Details), 1)
	detail, err := callError.Details[0].Message()
	requireNoError(t, err)
	retryDelay, ok := detail.(*durationpb.Duration)
	if !ok {
		t.Fatalf("detail is %T, want *durationpb.Duration", detail)
	}
	requireEqual(t, retryDelay.AsDuration(), 3*time.Second)
}

func TestUnimplemented(t *testing.T) {
	t.Parallel()
	// The server has no OwnerService, so it answers 404.
	server := connect.NewServer()
	apiv1connect.RegisterUserServiceHandler(server, &userService{})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	_, err := apiv1client.NewClient(httpServer.URL).OwnerService.GetOwner(t.Context(), &apiv1.GetOwnerRequest{})
	checkCode(t, err, apiv1client.CodeUnimplemented)
}

func TestDeadline(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(newServer(t).URL)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	user, err := client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "deadline"})
	requireNoError(t, err)
	requireEqual(t, user.GetName(), "5s")

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = client.UserService.GetUser(ctx, &apiv1.GetUserRequest{Id: "slow"})
	checkCode(t, err, apiv1client.CodeDeadlineExceeded)
}

func TestHeaders(t *testing.T) {
	t.Parallel()
	client := apiv1client.NewClient(newServer(t).URL, apiv1client.WithHeader("X-Client", "generated"))
	user, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	requireEqual(t, user.GetName(), "X-Client=generated User-Agent=connectclient-go")
}

func TestHook(t *testing.T) {
	t.Parallel()
	var procedure, contentType string
	var gotErr error
	client := apiv1client.NewClient(newServer(t).URL, apiv1client.WithHook(
		func(req *http.Request) (func(*http.Response, error), error) {
			req.Header.Set("X-Client", "hook")
			return func(res *http.Response, err error) {
				procedure, gotErr = req.URL.Path, err
				if res != nil {
					contentType = res.Header.Get("Content-Type")
				}
			}, nil
		},
	))
	user, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "headers"})
	requireNoError(t, err)
	requireEqual(t, user.GetName(), "X-Client=hook User-Agent=connectclient-go")
	requireEqual(t, procedure, apiv1client.UserServiceGetUserProcedure)
	requireEqual(t, contentType, "application/proto")
	requireEqual(t, gotErr, nil)

	_, err = client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "missing"})
	checkCode(t, err, apiv1client.CodeNotFound)
	checkCode(t, gotErr, apiv1client.CodeNotFound)
	requireEqual(t, contentType, "application/json")
}

func TestGzipResponse(t *testing.T) {
	t.Parallel()
	server := newServer(t, connecthttp.WithCompressMinBytes(1024))
	user, err := apiv1client.NewClient(server.URL).UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "large"})
	requireNoError(t, err)
	requireEqual(t, len(user.GetName()), 64<<10)
	_, err = apiv1client.NewClient(server.URL, apiv1client.WithReadMaxBytes(32<<10)).
		UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "large"})
	checkCode(t, err, apiv1client.CodeResourceExhausted)
}

func TestUnixSocket(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "cc")
	requireNoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "api.sock")
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
	client := apiv1client.NewClient("http://unix", apiv1client.WithHTTPClient(httpClient))
	user, err := client.UserService.GetUser(t.Context(), &apiv1.GetUserRequest{Id: "unix"})
	requireNoError(t, err)
	requireEqual(t, user.GetName(), "user unix")
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
	adminv1connect.RegisterAdminServiceHandler(server, &adminService{}) //nolint:staticcheck // the service is deprecated on purpose
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, options...)
	return mux
}

type userService struct {
	apiv1connect.UnimplementedUserServiceHandler
}

func (*userService) GetUser(ctx context.Context, req *apiv1.GetUserRequest) (*apiv1.GetUserResponse, error) {
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
		callInfo, ok := connect.CallInfoForServerContext(ctx)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, "no call info")
		}
		header := callInfo.RequestHeader()
		return &apiv1.GetUserResponse{
			Name: "X-Client=" + header.Get("X-Client") + " User-Agent=" + header.Get("User-Agent"),
		}, nil
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

func (*repositoryService) GetRepository(_ context.Context, req *apiv1.GetRepositoryRequest) (*apiv1.GetRepositoryResponse, error) {
	return &apiv1.GetRepositoryResponse{Repository: &apiv1.Repository{Name: req.GetName()}}, nil
}

type adminService struct {
	adminv1connect.UnimplementedAdminServiceHandler
}

func (*adminService) Health(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func checkCode(t *testing.T, err error, wantCode apiv1client.Code) {
	t.Helper()
	_ = requireCode(t, err, wantCode)
}

func requireCode(t *testing.T, err error, wantCode apiv1client.Code) *apiv1client.Error {
	t.Helper()
	callError, ok := errors.AsType[*apiv1client.Error](err)
	if !ok {
		t.Fatalf("error %v (%T) is not an *apiv1client.Error", err, err)
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
