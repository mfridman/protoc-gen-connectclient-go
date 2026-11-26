// Command protoc-gen-connectclient-go generates small, client-only Go code for
// Connect services. The generated code is built on connect-go v2 and the
// connectclient transport.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/mfridman/protoc-gen-connectclient-go/internal/plugin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

const usage = `Usage: protoc-gen-connectclient-go [--version | --help]

This program is a protoc plugin. Run it with buf or protoc.

Options (set with "opt" in buf.gen.yaml):
  package_suffix=<suffix>  Generate code into a sub-package with this suffix
                           (default "client"). An empty value generates code
                           into the package of the .pb.go files.
`

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-version":
			_, _ = fmt.Fprintln(os.Stdout, plugin.Name, version())
			return 0
		case "--help", "-help", "-h":
			_, _ = fmt.Fprint(os.Stdout, usage)
			return 0
		default:
			_, _ = fmt.Fprintf(os.Stderr, "%s: unknown argument %q\n\n%s", plugin.Name, args[0], usage)
			return 1
		}
	}
	if err := run(os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", plugin.Name, err)
		return 1
	}
	return 0
}

func run(stdin io.Reader, stdout io.Writer) error {
	input, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	request := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(input, request); err != nil {
		return fmt.Errorf("unmarshal request: %w", err)
	}
	response, err := plugin.Generate(request)
	if err != nil {
		return err
	}
	output, err := proto.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := stdout.Write(output); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

// version returns the module version from the build information, such as
// "v0.2.0" for go install, or a pseudo-version for a local build.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "devel"
	}
	return info.Main.Version
}
