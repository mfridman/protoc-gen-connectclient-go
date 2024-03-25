package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/bufbuild/protoplugin"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/plugin"
)

var version string

func main() {
	runArgs(os.Args[1:])

	ctx, stop := newContext()
	defer stop()

	environ := os.Environ()
	environ = append(environ, plugin.PLUGIN_VERSION+"="+getVersionFromBuildInfo())

	go func() {
		defer stop()
		if err := protoplugin.Run(
			ctx,
			protoplugin.Env{
				Args:    os.Args[1:],
				Environ: environ,
				Stdin:   os.Stdin,
				Stdout:  os.Stdout,
				Stderr:  os.Stderr,
			},
			protoplugin.HandlerFunc(plugin.Handle),
		); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", plugin.Name, err)
			os.Exit(1)
		}
	}()

	select {
	case <-ctx.Done():
		stop()
	}
}

func newContext() (context.Context, context.CancelFunc) {
	signals := []os.Signal{os.Interrupt}
	if runtime.GOOS != "windows" {
		signals = append(signals, syscall.SIGTERM)
	}
	return signal.NotifyContext(context.Background(), signals...)
}

func runArgs(args []string) error {
	for _, arg := range args {
		switch arg {
		case "--version", "-version":
			fmt.Fprintf(os.Stdout, "%s version: %s\n", plugin.Name, strings.TrimSpace(getVersionFromBuildInfo()))
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "%s: unknown argument: %s\n", plugin.Name, arg)
			os.Exit(1)
		}
	}
	return nil
}

// getVersionFromBuildInfo returns the version string from the build info, if available. It will
// always return a non-empty string.
//
//   - If the build info is not available, it returns "devel".
//   - If the main version is set, it returns the string as is.
//   - If building from source, it returns "devel" followed by the first 12 characters of the VCS
//     revision, followed by ", dirty" if the working directory was dirty. For example,
//     "devel (abcdef012345, dirty)" or "devel (abcdef012345)". If the VCS revision is not available,
//     "unknown revision" is used instead.
//
// Note, vcs info not stamped when built listing .go files directly. E.g.,
//   - `go build main.go`
//   - `go build .`
//
// For more information, see https://github.com/golang/go/issues/51279
func getVersionFromBuildInfo() string {
	if version != "" {
		return version
	}
	const defaultVersion = "devel"

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		// Should only happen if -buildvcs=false is set or using a really old version of Go.
		return defaultVersion
	}
	// The (devel) string is not documented, but it is the value used by the Go toolchain. See
	// https://github.com/golang/go/issues/29228
	if s := buildInfo.Main.Version; s != "" && s != "(devel)" {
		return buildInfo.Main.Version
	}
	var vcs struct {
		revision string
		time     time.Time
		modified bool
	}
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			vcs.revision = setting.Value
		case "vcs.time":
			vcs.time, _ = time.Parse(time.RFC3339, setting.Value)
		case "vcs.modified":
			vcs.modified = (setting.Value == "true")
		}
	}

	var b strings.Builder
	b.WriteString(defaultVersion)
	b.WriteString(" (")
	if vcs.revision == "" || len(vcs.revision) < 12 {
		b.WriteString("unknown revision")
	} else {
		b.WriteString(vcs.revision[:12])
	}
	if vcs.modified {
		b.WriteString(", dirty")
	}
	b.WriteString(")")
	return b.String()
}
