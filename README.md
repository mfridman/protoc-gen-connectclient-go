# protoc-gen-connectclient-go

A Protobuf plugin that generates a **single Connect Go client for all services** within a package,
along with several quality of life improvements. For more information, see the [But why?](#but-why)
section.

This plugin MUST be run with the `protoc-gen-go` plugin to generate the base types.

> [!NOTE]
>
> There's an experimental feature that uses wasm to generate the base types for the client. Which
> means this plugin is truly self-contained and is all you need.

## Installation

```shell
go install github.com/mfridman/protoc-gen-connectclient-go@latest
```

## Examples

See the [examples](./examples) directory for a simple end-end example of how to use this plugin.

## Plugin options

The plugin supports the following options:

| Option                                     | Description                             | Default |
| ------------------------------------------ | --------------------------------------- | ------- |
| [`seperate_packages`](#separate_packages)  | Generate into separate packages         | `false` |
| [`inclue_base_types`](#include_base_types) | Include base types in the output (wasm) | `false` |

### `separate_packages`

When set to `true`, the plugin will generate the client into a seperate package with the `client`
suffix.

### `include_base_types`

When set to `true`, the plugin will invoke the `protoc-gen-go` plugin to generate the base types for
the client. This is an experimental feature that uses wasm to generate the base types.

## But why?

I find myself writing a lot of Go clients against Connect services, often in CLIs or other small
applications. But I don't need the full power of the official Connect library.

Here's a quick list of what this library does and does not do:

- No streaming support
- No generics, just plain old structs
- Does not generate any Service-related code (very lightweight)
  - No runtime libraries, all generated code is self-contained
- No interceptors, just hooks for tapping into the request and response lifecycle
- No dependencies on the Connect runtime or generated code
  - It's just `POST` and `application/json` over HTTP using `http.DefaultClient` (you can override
    the default client)
  - Only one runtime dependency: `google.golang.org/protobuf`
- No need to maintain a separate client for each service
  - Just create a single client with `NewClient` and pass it around
- Functional options to tailor the client to your needs
  - Attach a token when the client is created, and it will be used for all requests
  - Attach an optional logger
  - Use a custom HTTP client, such as a retryable client like
    [hashicorp/go-retryablehttp](https://github.com/hashicorp/go-retryablehttp)

The premise is that you can use this library to make simple unary calls to Connect services without
writing a lot of boilerplate code. It's not meant to be a full replacement for the official Connect
library. If you need streaming, interceptors, or other advanced features, you should use the
official library.

## Gotchas

### Errors

The generated client does not return Connect errors. It returns an `*HTTPError` and the only field
that is gauranteed to be set is the `Code` field. Quite often Connect services will be mounted on a
`net/http` router and middleware may return a non-Connect error.

```go
type HTTPError struct {
	Procedure   string
	Code        int
	ConnectCode string
	Message     string
}
```

## Status

This is a work in progress. The plugin is functional and generates code that works. However, there
are still a few things that need to be done.

- [] Add tests
- [] Add wasm support for generating the base types
- [] Add method-level options, for more granular control
- [] Add logger support with debug and info levels
- [] See if there's low hanging fruit for performance improvements and the client adheres to best
  practices against Connect services
- [] See if it's possible to make the golden templates a reusable package, maybe one already exists?

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
