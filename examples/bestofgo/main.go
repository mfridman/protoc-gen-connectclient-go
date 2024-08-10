package main

import (
	"context"
	"fmt"
	"log"
	"text/tabwriter"
	"time"

	"github.com/mfridman/protoc-gen-connectclient-go/examples/bestofgo/gen/api"
	"github.com/mfridman/protoc-gen-connectclient-go/examples/bestofgo/gen/api/apiclient"
)

// A contrived example to demonstrate how to use the generated client with a custom context value
// and error handling.

const (
	ctxKey = "time.now"
)

func printDuration(ctx context.Context, err error) {
	v := ctx.Value(ctxKey).(time.Time)
	log.Printf("DEBUG: elapsed=%dms error=%v\n\n", time.Since(v).Milliseconds(), err)
}

func main() {
	log.SetFlags(0)
	client := apiclient.NewClient(
		"https://api.bestofgo.dev",
		apiclient.WithCheckError(printDuration),
	)

	ctx := context.Background()
	now := time.Now()
	ctx = context.WithValue(ctx, ctxKey, now)
	resp, err := client.APIService.GetTopReposByYear(ctx, &api.GetTopReposByYearRequest{
		Year: 2023,
	})
	if err != nil {
		log.Fatal(err)
	}
	tabwriter := tabwriter.NewWriter(log.Writer(), 0, 0, 1, ' ', 0)
	for _, repo := range resp.RepoResults[:5] {
		tabwriter.Write([]byte(fmt.Sprintf("%s\t%d\n", repo.GetRepo().GetRepoFullName(), repo.GetYearCount())))
	}
	tabwriter.Flush()
}
