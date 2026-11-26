# examples

[eliza](eliza/main.go) calls the Connect demo service at `demo.connectrpc.com`. No account is needed:

```shell
go run ./eliza
```

The generated code is committed. To generate it again, run `make examples` from the root of the repository.

This directory is a separate Go module, so the example does not add dependencies to the main module.
