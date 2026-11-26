// Command lean calls a unary RPC with a client from
// protoc-gen-connectclient-go.
package main

import (
	"context"
	"fmt"
	"os"

	apiv1 "github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1/apiv1client"
)

func main() {
	client := apiv1client.NewClient(os.Getenv("SERVER_URL"))
	res, err := client.UserService.GetUser(context.Background(), &apiv1.GetUserRequest{Id: os.Args[0]})
	fmt.Println(res, err)
}
