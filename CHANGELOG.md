# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Breaking:** generated code builds on connect-go v2 and the new `connectclient` transport. Create a client with `NewClient(connectclient.New(url, ...))`
- **Breaking:** failed calls return `*connect.Error` instead of `*HTTPError`
- **Breaking:** code goes into a `<package>client` sub-package by default. `package_suffix`, as in `protoc-gen-connect-go`, replaces `separate_packages`; set `package_suffix=` to keep the old layout
- **Breaking:** `WithHeader` and interceptors replace `WithModifyRequest` and `WithCheckError`
- Require Go 1.26 or newer

### Added

- Deadlines, error details, headers, trailers, interceptors, and a response size limit
- Support for Protobuf editions
- The `connectclient` transport also works with clients from `protoc-gen-connect-go` v2

### Fixed

- Request and response types come from the method definition, not from the method name
