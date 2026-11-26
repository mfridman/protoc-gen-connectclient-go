// Command stock calls a unary RPC with a stock connect-go v2 client and the
// connecthttp transport.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	apiv1 "github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1/apiv1connect"
)

func main() {
	transport := connecthttp.NewTransport(http.DefaultClient, os.Getenv("SERVER_URL"))
	client := apiv1connect.NewUserServiceClient(connect.NewClient(transport))
	res, err := client.GetUser(context.Background(), &apiv1.GetUserRequest{Id: os.Args[0]})
	fmt.Println(res, err)
}
