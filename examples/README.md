# examples

This directory contains a few examples of how to use the `protoc-gen-connectclient-go` plugin. We'll
use the [eliza](#eliza) client as an example. The same steps apply to any other example, just change
the name of the example.

## Eliza

See the [eliza/buf.gen.yaml](eliza/buf.gen.yaml) file for an example of how to use this plugin with
`buf` for code generation.

Assming you have:

1. `buf` installed
2. In the root of the repository

Run:

```shell
make examples
```

Under the hood, this will run:

```shell
buf generate buf.build/connectrpc/eliza --template ./examples/eliza/buf.gen.yaml --include-imports
```

This will generate the client Go code for the `eliza` service in the
[buf.build/connectrpc/eliza](https://buf.build/connectrpc/eliza) module.

In the [eliza/main.go](eliza/main.go) file you can see an example of how to use the generated client
code. Here's a snippet:

```go
client := elizav1.NewClient("https://demo.connectrpc.com")
resp, err := client.ElizaService.Say(context.Background(), &elizav1.SayRequest{
	Sentence: "Hello",
})
if err != nil {
	log.Fatal(err)
}
log.Println("Response:", resp.Sentence)
```

And if you run the example:

```shell
go run ./examples/eliza
```

You should see the response from the Eliza service.

```
Response: Hello there...how are you today?
```
