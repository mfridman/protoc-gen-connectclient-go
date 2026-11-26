# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Breaking:** code goes into a `<package>client` sub-package by default. `package_suffix`, as in `protoc-gen-connect-go`, replaces `separate_packages`; set `package_suffix=` to keep the old layout
- **Breaking:** `ClientOption` is renamed to `Option`
- **Breaking:** failed calls return an `*Error` with the Connect code, message, and details, instead of `*HTTPError`
- **Breaking:** `WithHook` replaces `WithModifyRequest` and `WithCheckError`. It runs before each call and can return a function that runs after the call, with the response and the error
- Require Go 1.26 or newer

### Added

- Deadlines and error details
- `WithHeader` for headers on every call, and `WithReadMaxBytes` to limit the response size
- Support for Protobuf editions
- Tested with the Connect conformance suite

### Fixed

- Request and response types come from the method definition, not from the method name
