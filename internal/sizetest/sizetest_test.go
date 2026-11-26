// Package sizetest checks that programs built with connectclient stay small.
package sizetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modulePath = "github.com/mfridman/protoc-gen-connectclient-go"

// TestDependencies checks that a program with a generated client and the
// connectclient transport does not link the heavy parts of connect-go.
func TestDependencies(t *testing.T) {
	t.Parallel()
	output, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "./testdata/lean").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	deps := strings.Fields(string(output))
	for _, forbidden := range []string{
		"connectrpc.com/connect/v2/connecthttp",
		"connectrpc.com/connect/v2/connectproto",
		"connectrpc.com/connect/v2/connectgzip",
		"google.golang.org/protobuf/encoding/protojson",
	} {
		if slices.Contains(deps, forbidden) {
			t.Errorf("lean program depends on %s", forbidden)
		}
	}
	for _, required := range []string{"connectrpc.com/connect/v2", modulePath + "/connectclient"} {
		if !slices.Contains(deps, required) {
			t.Errorf("lean program does not depend on %s", required)
		}
	}
}

// TestBinarySize builds the test programs and checks that connectclient adds
// much less to a binary than connecthttp.
func TestBinarySize(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three binaries")
	}
	t.Parallel()
	sizes := make(map[string]int64)
	for _, program := range []string{"baseline", "lean", "stock"} {
		sizes[program] = buildStripped(t, program)
	}
	lean := sizes["lean"] - sizes["baseline"]
	stock := sizes["stock"] - sizes["baseline"]
	t.Logf("stripped bytes added to a net/http + protobuf program: connectclient %d (%d KiB), connecthttp %d (%d KiB)",
		lean, lean>>10, stock, stock>>10)
	// Stripped sizes move in 64 KiB steps. The limits leave room for changes
	// in Go and connect-go, but fail if connectclient starts to link a large
	// dependency. See leanLimit.
	if lean > leanLimit {
		t.Errorf("connectclient adds %d KiB, want at most %d KiB", lean>>10, leanLimit>>10)
	}
	if stock < 3*lean {
		t.Errorf("connecthttp adds %d KiB and connectclient %d KiB, want connectclient at most a third", stock>>10, lean>>10)
	}
}

func buildStripped(t *testing.T, program string) int64 {
	t.Helper()
	output := filepath.Join(t.TempDir(), program)
	command := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-ldflags=-s -w", "-o", output, "./testdata/"+program)
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", program, err, combined)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
