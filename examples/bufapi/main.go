package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	registryv1alpha1 "github.com/mfridman/protoc-gen-connectclient-go/examples/bufapi/gen/buf/alpha/registry/v1alpha1"
)

// Try running this with a BUF_TOKEN environment variable set to a valid token.
//
//  1. Run make examples
// 	2. Run this program: BUF_TOKEN=<token> go run ./examples/bufapi

func main() {
	log.SetFlags(0)
	client := registryv1alpha1.NewClient(
		"https://buf.build",
		registryv1alpha1.WithModifyRequest(func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+os.Getenv("BUF_TOKEN"))
			return nil
		}),
	)
	resp, err := client.AuthnService.GetCurrentUser(context.Background(), &registryv1alpha1.GetCurrentUserRequest{})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Response:", resp.User.Username, "created on", resp.User.CreateTime.AsTime().Format(time.Stamp))
}
