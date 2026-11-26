// Command baseline sends a Protobuf message with net/http and no RPC
// framework, and decodes a JSON error body with encoding/json, as most
// programs already use it. The size tests measure the other programs against
// it.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	apiv1 "github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen/api/v1"
	"google.golang.org/protobuf/proto"
)

func main() {
	body, err := proto.Marshal(&apiv1.GetUserRequest{Id: os.Args[0]})
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := http.Post(os.Getenv("SERVER_URL")+"/api.v1.UserService/GetUser", "application/proto", bytes.NewReader(body))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		var connectError struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		fmt.Println(json.Unmarshal(data, &connectError), connectError)
		return
	}
	var res apiv1.GetUserResponse
	fmt.Println(proto.Unmarshal(data, &res), &res)
}
