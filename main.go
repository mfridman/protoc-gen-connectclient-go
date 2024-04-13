package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/bufbuild/protoplugin"
	"github.com/mfridman/buildversion"
	"github.com/mfridman/protoc-gen-connectclient-go/internal/plugin"
)

var version string

func main() {
	runArgs(os.Args[1:])

	ctx, stop := newContext()
	defer stop()

	environ := os.Environ()
	environ = append(environ, plugin.PLUGIN_VERSION+"="+buildversion.New(version))

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
			fmt.Fprintf(os.Stdout, "%s version: %s\n", plugin.Name, buildversion.New(version))
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "%s: unknown argument: %s\n", plugin.Name, arg)
			os.Exit(1)
		}
	}
	return nil
}
