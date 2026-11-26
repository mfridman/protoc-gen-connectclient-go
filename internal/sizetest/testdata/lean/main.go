// Command lean calls a unary RPC with a generated client and the
// connectclient transport.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mfridman/protoc-gen-connectclient-go/connectclient"
	apiv1 "github.com/mfridman/protoc-gen-connectclient-go/internal/testdata/gen/api/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/testdata/gen/api/v1/apiv1client"
)

func main() {
	client := apiv1client.NewClient(connectclient.New(os.Getenv("SERVER_URL")))
	res, err := client.UserService.GetUser(context.Background(), &apiv1.GetUserRequest{Id: os.Args[0]})
	fmt.Println(res, err)
}
