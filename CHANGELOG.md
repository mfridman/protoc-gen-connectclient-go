# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Breaking:** code goes into a `<package>client` sub-package by default. `package_suffix`, as in `protoc-gen-connect-go`, replaces `separate_packages`; set `package_suffix=` to keep the old layout
- **Breaking:** failed calls return an `*Error` with the Connect code, message, and details, instead of `*HTTPError`
- **Breaking:** `WithHeader` replaces `WithModifyRequest`, and `WithAfterCall` replaces `WithCheckError`
- Require Go 1.26 or newer

### Added

- Deadlines, error details, and a response size limit
- Support for Protobuf editions
- Tested with the Connect conformance suite

### Fixed

- Request and response types come from the method definition, not from the method name
