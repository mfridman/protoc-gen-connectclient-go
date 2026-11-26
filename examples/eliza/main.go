package main

import (
	"context"
	"log"

	elizav1 "github.com/mfridman/protoc-gen-connectclient-go/examples/eliza/gen/connectrpc/eliza/v1"
	"github.com/mfridman/protoc-gen-connectclient-go/examples/eliza/gen/connectrpc/eliza/v1/elizav1client"
)

func main() {
	log.SetFlags(0)
	client := elizav1client.NewClient("https://demo.connectrpc.com")
	resp, err := client.ElizaService.Say(context.Background(), &elizav1.SayRequest{
		Sentence: "Hello",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Response:", resp.GetSentence())
}
