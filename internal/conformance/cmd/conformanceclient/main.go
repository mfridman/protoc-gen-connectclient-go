// Command conformanceclient is a client under test for the Connect
// conformance suite (https://github.com/connectrpc/conformance). It calls the
// reference server with a client from protoc-gen-connectclient-go and the
// connectclient transport.
//
// It reads size-delimited ClientCompatRequest messages from stdin and writes
// ClientCompatResponse messages to stdout.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/mfridman/protoc-gen-connectclient-go/connectclient"
	conformancev1 "github.com/mfridman/protoc-gen-connectclient-go/internal/conformance/gen/connectrpc/conformance/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/conformance/gen/connectrpc/conformance/v1/conformancev1client"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const maxMessageSize = 64 << 20 // 64 MiB

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := run(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "conformanceclient:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	reader := bufio.NewReader(stdin)
	var writeMutex sync.Mutex
	var writeErr error
	var waitGroup sync.WaitGroup
	requests := make(chan *conformancev1.ClientCompatRequest)
	httpClients := &httpClientCache{}
	for range runtime.GOMAXPROCS(0) * 4 {
		waitGroup.Go(func() {
			for request := range requests {
				response := invoke(ctx, httpClients, request)
				writeMutex.Lock()
				if err := writeDelimited(stdout, response); err != nil && writeErr == nil {
					writeErr = err
				}
				writeMutex.Unlock()
			}
		})
	}
	readErr := readRequests(reader, requests)
	close(requests)
	waitGroup.Wait()
	return errors.Join(readErr, writeErr)
}

func readRequests(reader io.Reader, requests chan<- *conformancev1.ClientCompatRequest) error {
	for {
		request := &conformancev1.ClientCompatRequest{}
		if err := readDelimited(reader, request); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		requests <- request
	}
}

func invoke(
	ctx context.Context,
	httpClients *httpClientCache,
	request *conformancev1.ClientCompatRequest,
) *conformancev1.ClientCompatResponse {
	result, err := invokeRPC(ctx, httpClients, request)
	response := &conformancev1.ClientCompatResponse{TestName: request.GetTestName()}
	if err != nil {
		response.Result = &conformancev1.ClientCompatResponse_Error{
			Error: &conformancev1.ClientErrorResult{Message: err.Error()},
		}
		return response
	}
	response.Result = &conformancev1.ClientCompatResponse_Response{Response: result}
	return response
}

func invokeRPC(
	ctx context.Context,
	httpClients *httpClientCache,
	request *conformancev1.ClientCompatRequest,
) (*conformancev1.ClientResponseResult, error) {
	if request.GetService() != conformancev1client.ConformanceServiceName {
		return nil, fmt.Errorf("unknown service %q", request.GetService())
	}
	if request.GetProtocol() != conformancev1.Protocol_PROTOCOL_CONNECT ||
		request.GetCodec() != conformancev1.Codec_CODEC_PROTO {
		return nil, fmt.Errorf("unsupported protocol %v or codec %v", request.GetProtocol(), request.GetCodec())
	}
	if len(request.GetRequestMessages()) != 1 {
		return nil, fmt.Errorf("unary calls need one request message, got %d", len(request.GetRequestMessages()))
	}
	httpClient, baseURL, err := httpClients.get(request)
	if err != nil {
		return nil, err
	}
	client := conformancev1client.NewClient(connectclient.New(
		baseURL,
		connectclient.WithHTTPClient(httpClient),
		connectclient.WithReadMaxBytes(int64(request.GetMessageReceiveLimit())),
	))

	if request.TimeoutMs != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.GetTimeoutMs())*time.Millisecond)
		defer cancel()
	}
	ctx, callInfo := connect.NewClientContext(ctx)
	for _, header := range request.GetRequestHeaders() {
		for _, value := range header.GetValue() {
			callInfo.RequestHeader().Add(header.GetName(), value)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if cancelTiming := request.GetCancel(); cancelTiming != nil {
		switch timing := cancelTiming.GetCancelTiming().(type) {
		case *conformancev1.ClientCompatRequest_Cancel_BeforeCloseSend:
			cancel()
		case *conformancev1.ClientCompatRequest_Cancel_AfterCloseSendMs:
			time.AfterFunc(time.Duration(timing.AfterCloseSendMs)*time.Millisecond, cancel)
		case nil:
			cancel()
		default:
			return nil, fmt.Errorf("unsupported cancel timing %T for a unary call", timing)
		}
	}

	var payload *conformancev1.ConformancePayload
	requestMessage := request.GetRequestMessages()[0]
	switch request.GetMethod() {
	case "Unary":
		unaryRequest := &conformancev1.UnaryRequest{}
		if err := requestMessage.UnmarshalTo(unaryRequest); err != nil {
			return nil, err
		}
		var response *conformancev1.UnaryResponse
		response, err = client.ConformanceService.Unary(ctx, unaryRequest)
		payload = response.GetPayload()
	case "IdempotentUnary":
		idempotentRequest := &conformancev1.IdempotentUnaryRequest{}
		if err := requestMessage.UnmarshalTo(idempotentRequest); err != nil {
			return nil, err
		}
		var response *conformancev1.IdempotentUnaryResponse
		response, err = client.ConformanceService.IdempotentUnary(ctx, idempotentRequest)
		payload = response.GetPayload()
	case "Unimplemented":
		unimplementedRequest := &conformancev1.UnimplementedRequest{}
		if err := requestMessage.UnmarshalTo(unimplementedRequest); err != nil {
			return nil, err
		}
		_, err = client.ConformanceService.Unimplemented(ctx, unimplementedRequest)
	default:
		return nil, fmt.Errorf("unsupported method %q", request.GetMethod())
	}

	result := &conformancev1.ClientResponseResult{
		ResponseHeaders:  protoHeaders(callInfo.ResponseHeader()),
		ResponseTrailers: protoHeaders(callInfo.ResponseTrailer()),
	}
	if err != nil {
		result.Error = protoError(err)
	} else {
		if payload == nil {
			payload = &conformancev1.ConformancePayload{}
		}
		result.Payloads = []*conformancev1.ConformancePayload{payload}
	}
	return result, nil
}

func protoHeaders(header *connect.Header) []*conformancev1.Header {
	var headers []*conformancev1.Header
	for name, values := range header.All() {
		headers = append(headers, &conformancev1.Header{Name: name, Value: values})
	}
	return headers
}

func protoError(err error) *conformancev1.Error {
	connectError, ok := errors.AsType[*connect.Error](err)
	if !ok {
		return &conformancev1.Error{
			Code:    conformancev1.Code_CODE_UNKNOWN,
			Message: proto.String(err.Error()),
		}
	}
	result := &conformancev1.Error{
		Code:    conformancev1.Code(connectError.Code()),
		Message: proto.String(connectError.Message()),
	}
	for _, detail := range connectError.Details() {
		result.Details = append(result.Details, &anypb.Any{
			TypeUrl: "type.googleapis.com/" + detail.Type,
			Value:   detail.Value,
		})
	}
	return result
}

// httpClientCache shares an HTTP client per HTTP version and TLS setup, so
// calls reuse connections.
type httpClientCache struct {
	mutex   sync.Mutex
	clients map[httpClientKey]*http.Client
}

type httpClientKey struct {
	httpVersion   conformancev1.HTTPVersion
	serverTLSCert string
	clientTLSCert string
	clientTLSKey  string
}

func (c *httpClientCache) get(request *conformancev1.ClientCompatRequest) (*http.Client, string, error) {
	key := httpClientKey{
		httpVersion:   request.GetHttpVersion(),
		serverTLSCert: string(request.GetServerTlsCert()),
		clientTLSCert: string(request.GetClientTlsCreds().GetCert()),
		clientTLSKey:  string(request.GetClientTlsCreds().GetKey()),
	}
	scheme := "http"
	if key.serverTLSCert != "" {
		scheme = "https"
	}
	baseURL := scheme + "://" + net.JoinHostPort(request.GetHost(), strconv.Itoa(int(request.GetPort())))
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if httpClient, ok := c.clients[key]; ok {
		return httpClient, baseURL, nil
	}
	httpClient, err := newHTTPClient(key)
	if err != nil {
		return nil, "", err
	}
	if c.clients == nil {
		c.clients = make(map[httpClientKey]*http.Client)
	}
	c.clients[key] = httpClient
	return httpClient, baseURL, nil
}

func newHTTPClient(key httpClientKey) (*http.Client, error) {
	transport := &http.Transport{Protocols: &http.Protocols{}}
	if key.serverTLSCert != "" {
		tlsConfig, err := newTLSConfig(key)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}
	switch key.httpVersion {
	case conformancev1.HTTPVersion_HTTP_VERSION_1:
		transport.Protocols.SetHTTP1(true)
	case conformancev1.HTTPVersion_HTTP_VERSION_2:
		if key.serverTLSCert != "" {
			transport.Protocols.SetHTTP2(true)
		} else {
			transport.Protocols.SetUnencryptedHTTP2(true)
		}
	default:
		return nil, fmt.Errorf("unsupported HTTP version %v", key.httpVersion)
	}
	return &http.Client{Transport: transport}, nil
}

func newTLSConfig(key httpClientKey) (*tls.Config, error) {
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM([]byte(key.serverTLSCert)) {
		return nil, errors.New("parse server TLS certificate")
	}
	tlsConfig := &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12}
	if key.clientTLSCert != "" {
		certificate, err := tls.X509KeyPair([]byte(key.clientTLSCert), []byte(key.clientTLSKey))
		if err != nil {
			return nil, fmt.Errorf("parse client TLS credentials: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

func readDelimited(reader io.Reader, message proto.Message) error {
	var prefix [4]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(prefix[:])
	if size > maxMessageSize {
		return fmt.Errorf("message of %d bytes is larger than %d", size, maxMessageSize)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return fmt.Errorf("read message: %w", err)
	}
	return proto.Unmarshal(data, message)
}

func writeDelimited(writer io.Writer, message proto.Message) error {
	data, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(data)))
	if _, err := writer.Write(prefix[:]); err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}
