# examples

This directory has examples of the code that `protoc-gen-connectclient-go` generates. It is a separate Go module, so the examples do not add dependencies to the main module.

| Example | Service | Notes |
|---|---|---|
| [eliza](eliza/main.go) | [buf.build/connectrpc/eliza](https://buf.build/connectrpc/eliza) on `demo.connectrpc.com` | No account needed. |

## Generate the code

The generated code is committed. To generate it again, run this from the root of the repository:

```shell
make examples
```

This builds the plugin and runs `buf generate` with each example's `buf.gen.yaml`. For example, [eliza/buf.gen.yaml](eliza/buf.gen.yaml) generates the client for the Eliza service.

## Run an example

From this directory, run:

```shell
go run ./eliza
```

The output is similar to:

```
Response: Hello...I'm glad you could drop by today.
```

The client code is short:

```go
client := elizav1client.NewClient("https://demo.connectrpc.com")
resp, err := client.ElizaService.Say(context.Background(), &elizav1.SayRequest{
	Sentence: "Hello",
})
```
