package main

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mfridman/protoc-gen-connectclient-go/connectclient"
	registryv1alpha1 "github.com/mfridman/protoc-gen-connectclient-go/examples/bufapi/gen/buf/alpha/registry/v1alpha1"
	"github.com/mfridman/protoc-gen-connectclient-go/examples/bufapi/gen/buf/alpha/registry/v1alpha1/registryv1alpha1client"
)

var (
	DEBUG         = os.Getenv("DEBUG") == "1"
	defaultRemote = "buf.build"
)

// Try running this with a BUF_TOKEN environment variable set to a valid token.
//
//  1. Run make examples
//  2. Run this program from the examples directory: BUF_TOKEN=<token> go run ./bufapi

func main() {
	log.SetFlags(0)

	remote := cmp.Or(os.Getenv("BUF_REMOTE"), defaultRemote)

	token := os.Getenv("BUF_TOKEN")
	if token == "" {
		var err error
		token, err = parseNetrc(remote)
		if err != nil && DEBUG {
			log.Println("failed to parse .netrc:", err)
		}
	}

	client := registryv1alpha1client.NewClient(connectclient.New(
		"https://"+remote,
		connectclient.WithHeader("Authorization", "Bearer "+token),
	))
	resp, err := client.AuthnService.GetCurrentUser(context.Background(), &registryv1alpha1.GetCurrentUserRequest{})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Response:", resp.GetUser().GetUsername(), "created on", resp.GetUser().GetCreateTime().AsTime().Format(time.DateTime))
}

func parseNetrc(machineName string) (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	r, err := os.Open(filepath.Join(dir, ".netrc"))
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(r)
	var gotcha bool
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "machine" && len(fields) > 1 && fields[1] == machineName {
			gotcha = true
			continue
		}
		if gotcha {
			if fields[0] == "password" && len(fields) > 1 {
				return fields[1], nil
			}
			if fields[0] == "machine" {
				// Reached the end of the machine block without finding a password.
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no password found for %s", machineName)
}
