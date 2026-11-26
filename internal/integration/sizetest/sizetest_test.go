// Package sizetest checks that programs with generated clients stay small.
package sizetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/mfridman/protoc-gen-connectclient-go/internal/integration"

// TestDependencies checks that a program with a generated client only
// depends on the standard library and google.golang.org/protobuf.
func TestDependencies(t *testing.T) {
	t.Parallel()
	output, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "./testdata/lean").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	for _, dep := range strings.Fields(string(output)) {
		firstElement, _, _ := strings.Cut(dep, "/")
		if strings.Contains(firstElement, ".") &&
			!strings.HasPrefix(dep, "google.golang.org/protobuf/") &&
			!strings.HasPrefix(dep, modulePath+"/") {
			t.Errorf("lean program depends on %s", dep)
		}
	}
}

// TestBinarySize builds the test programs and checks that a generated client
// adds much less to a binary than connect-go with connecthttp.
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
	t.Logf("stripped bytes added to a net/http + protobuf + encoding/json program: generated client %d (%d KiB), connecthttp %d (%d KiB)",
		lean, lean>>10, stock, stock>>10)
	// Stripped sizes move in 64 KiB steps. The limits leave room for changes
	// in Go and connect-go, but fail if the generated client starts to link a
	// large dependency.
	if lean > 128<<10 {
		t.Errorf("generated client adds %d KiB, want at most 128 KiB", lean>>10)
	}
	if stock < 3*lean {
		t.Errorf("connecthttp adds %d KiB and the generated client %d KiB, want at most a third", stock>>10, lean>>10)
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
