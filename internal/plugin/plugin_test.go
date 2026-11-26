package plugin

import (
	gomaps "maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	testdataDir    = "../testdata"
	testImportPath = "github.com/mfridman/protoc-gen-connectclient-go/internal/testdata/gen"
)

// testFiles are the .proto files in internal/proto, with the Go packages
// that buf's managed mode gives them in internal/proto/buf.gen.yaml.
var testFiles = map[string]string{
	"admin/v1/admin.proto":        testImportPath + "/admin/v1;adminv1",
	"api/v1/repository.proto":     testImportPath + "/api/v1;apiv1",
	"api/v1/user_owner.proto":     testImportPath + "/api/v1;apiv1",
	"google/protobuf/empty.proto": "",
}

// TestGolden checks that the plugin generates the files committed in
// internal/testdata/gen. Run "make proto" to update them.
func TestGolden(t *testing.T) {
	t.Parallel()
	response := generateTest(t, "paths=source_relative")
	requireNoResponseError(t, response)
	var wantFiles []string
	err := filepath.WalkDir(filepath.Join(testdataDir, "gen"), func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, generatedFilenameExtension) {
			relativePath, err := filepath.Rel(filepath.Join(testdataDir, "gen"), path)
			if err != nil {
				return err
			}
			wantFiles = append(wantFiles, filepath.ToSlash(relativePath))
		}
		return nil
	})
	requireNoError(t, err)
	var gotFiles []string
	for _, file := range response.GetFile() {
		gotFiles = append(gotFiles, file.GetName())
		want, err := os.ReadFile(filepath.Join(testdataDir, "gen", filepath.FromSlash(file.GetName())))
		requireNoError(t, err)
		if file.GetContent() != string(want) {
			t.Errorf("%s differs from the committed file; run make proto\ngot:\n%s", file.GetName(), file.GetContent())
		}
	}
	slices.Sort(gotFiles)
	slices.Sort(wantFiles)
	if !slices.Equal(gotFiles, wantFiles) {
		t.Fatalf("generated files %v, want %v", gotFiles, wantFiles)
	}
}

func TestEmptyPackageSuffix(t *testing.T) {
	t.Parallel()
	request := newTestRequest(t, "paths=source_relative,package_suffix=")
	// admin/v1 declares a Client message; see TestErrors.
	request.FileToGenerate = []string{"api/v1/repository.proto", "api/v1/user_owner.proto"}
	response, err := Generate(request)
	requireNoError(t, err)
	requireNoResponseError(t, response)
	got := responseFiles(response)
	want := []string{
		"api/v1/apiv1.connectclient.go",
		"api/v1/repository.connectclient.go",
		"api/v1/user_owner.connectclient.go",
	}
	if !slices.Equal(slices.Sorted(gomaps.Keys(got)), want) {
		t.Fatalf("generated files %v, want %v", slices.Sorted(gomaps.Keys(got)), want)
	}
	content := got["api/v1/user_owner.connectclient.go"]
	requireContains(t, content, "package apiv1\n")
	requireContains(t, content, "req *GetUserRequest) (*GetUserResponse, error)")
	requireNotContains(t, content, testImportPath+"/api/v1\"")
	requireContains(t, got["api/v1/apiv1.connectclient.go"], "package apiv1\n")
}

func TestCustomPackageSuffix(t *testing.T) {
	t.Parallel()
	response := generateTest(t, "paths=source_relative,package_suffix=rpc")
	requireNoResponseError(t, response)
	got := responseFiles(response)
	content, ok := got["api/v1/apiv1rpc/user_owner.connectclient.go"]
	if !ok {
		t.Fatalf("missing api/v1/apiv1rpc/user_owner.connectclient.go in %v", slices.Sorted(gomaps.Keys(got)))
	}
	requireContains(t, content, "package apiv1rpc\n")
	requireContains(t, content, `v1 "`+testImportPath+`/api/v1"`)
	requireContains(t, got["api/v1/apiv1rpc/apiv1rpc.connectclient.go"], "type Client struct {")
}

func TestImportPaths(t *testing.T) {
	t.Parallel()
	// Without paths=source_relative, files go into directories named after the
	// Go import path.
	response := generateTest(t, "")
	requireNoResponseError(t, response)
	got := responseFiles(response)
	if _, ok := got[testImportPath+"/api/v1/apiv1client/user_owner.connectclient.go"]; !ok {
		t.Fatalf("missing import-path file in %v", slices.Sorted(gomaps.Keys(got)))
	}
}

func TestContent(t *testing.T) {
	t.Parallel()
	response := generateTest(t, "paths=source_relative")
	requireNoResponseError(t, response)
	got := responseFiles(response)
	repository := got["api/v1/apiv1client/repository.connectclient.go"]
	requireContains(t, repository, "IdempotencyLevel: connect.IdempotencyNoSideEffects,")
	requireContains(t, repository, "// Streaming RPCs are not generated: WatchRepository.")
	requireNotContains(t, repository, "WatchRepositoryProcedure")
	requireNotContains(t, repository, "EventService")
	requireContains(t, repository, `connect "connectrpc.com/connect/v2"`)
	userOwner := got["api/v1/apiv1client/user_owner.connectclient.go"]
	requireContains(t, userOwner, "IdempotencyLevel: connect.IdempotencyIdempotent,")
	requireContains(t, userOwner, "// LegacyGetUser returns a user by ID.\n//\n// Deprecated: do not use.\nfunc (c *UserServiceClient) LegacyGetUser(")
	requireContains(t, userOwner, "req *v1.EchoMessage) (*v1.EchoMessage, error)")
	admin := got["admin/v1/adminv1client/admin.connectclient.go"]
	requireContains(t, admin, "req *emptypb.Empty) (*emptypb.Empty, error)")
	requireContains(t, admin, "// Deprecated: do not use.\ntype AdminServiceClient struct")
	requireContains(t, admin, "// The admin API tests editions and messages from another package.")
	packageClient := got["api/v1/apiv1client/apiv1client.connectclient.go"]
	requireContains(t, packageClient, "RepositoryService *RepositoryServiceClient\n")
	requireContains(t, packageClient, "UserService       *UserServiceClient\n")
	requireContains(t, packageClient, "OwnerService      *OwnerServiceClient\n")
	requireNotContains(t, packageClient, "EventService")
}

func TestNoServices(t *testing.T) {
	t.Parallel()
	request := newTestRequest(t, "paths=source_relative")
	request.FileToGenerate = []string{"google/protobuf/empty.proto"}
	response, err := Generate(request)
	requireNoError(t, err)
	requireNoResponseError(t, response)
	if len(response.GetFile()) != 0 {
		t.Fatalf("generated %d files, want none", len(response.GetFile()))
	}
}

func TestFeatures(t *testing.T) {
	t.Parallel()
	response := generateTest(t, "paths=source_relative")
	wantFeatures := uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL |
		pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
	if response.GetSupportedFeatures() != wantFeatures {
		t.Fatalf("supported features = %d, want %d", response.GetSupportedFeatures(), wantFeatures)
	}
	if response.GetMaximumEdition() != int32(descriptorpb.Edition_EDITION_2024) {
		t.Fatalf("maximum edition = %d", response.GetMaximumEdition())
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		parameter string
		files     []string
		wantError string
	}{
		{
			name:      "invalid_package_suffix",
			parameter: "package_suffix=1bad",
			wantError: `package_suffix "1bad" is not a valid Go identifier`,
		},
		{
			name:      "unknown_option",
			parameter: "separate_packages=true",
			wantError: "separate_packages",
		},
		{
			name:      "name_collision",
			parameter: "paths=source_relative,package_suffix=",
			wantError: "generated name Client is already declared in Go package " + testImportPath + "/admin/v1",
		},
		{
			// The Client file for package "repository" would be named
			// repository.connectclient.go, the same as the file for
			// repository.proto.
			name:      "filename_collision",
			parameter: "paths=source_relative,package_suffix=,Mapi/v1/repository.proto=example.com/api/v1;repository,Mapi/v1/user_owner.proto=example.com/api/v1;repository",
			files:     []string{"api/v1/repository.proto", "api/v1/user_owner.proto"},
			wantError: "a .proto file generates the same name",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := newTestRequest(t, test.parameter)
			if test.files != nil {
				request.FileToGenerate = test.files
			}
			response, err := Generate(request)
			requireNoError(t, err)
			if !strings.Contains(response.GetError(), test.wantError) {
				t.Fatalf("error = %q, want it to contain %q", response.GetError(), test.wantError)
			}
		})
	}
}

func generateTest(t *testing.T, parameter string) *pluginpb.CodeGeneratorResponse {
	t.Helper()
	response, err := Generate(newTestRequest(t, parameter))
	requireNoError(t, err)
	return response
}

// newTestRequest returns a request for the .proto files in internal/proto,
// read from the descriptor set that "make proto" writes.
func newTestRequest(t *testing.T, parameter string) *pluginpb.CodeGeneratorRequest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, "image.binpb"))
	requireNoError(t, err)
	fileDescriptorSet := &descriptorpb.FileDescriptorSet{}
	requireNoError(t, proto.Unmarshal(data, fileDescriptorSet))
	var parameters []string
	if parameter != "" {
		parameters = append(parameters, parameter)
	}
	var filesToGenerate []string
	for _, file := range fileDescriptorSet.GetFile() {
		goPackage, ok := testFiles[file.GetName()]
		if !ok {
			continue
		}
		if goPackage != "" && !strings.Contains(parameter, "M"+file.GetName()+"=") {
			parameters = append(parameters, "M"+file.GetName()+"="+goPackage)
		}
		if !strings.HasPrefix(file.GetName(), "google/") {
			filesToGenerate = append(filesToGenerate, file.GetName())
		}
	}
	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate: filesToGenerate,
		Parameter:      proto.String(strings.Join(parameters, ",")),
		ProtoFile:      fileDescriptorSet.GetFile(),
	}
}

func responseFiles(response *pluginpb.CodeGeneratorResponse) map[string]string {
	files := make(map[string]string)
	for _, file := range response.GetFile() {
		files[file.GetName()] = file.GetContent()
	}
	return files
}

func requireNoResponseError(t *testing.T, response *pluginpb.CodeGeneratorResponse) {
	t.Helper()
	if response.GetError() != "" {
		t.Fatalf("response error: %s", response.GetError())
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireContains(t *testing.T, content, substring string) {
	t.Helper()
	if !strings.Contains(content, substring) {
		t.Fatalf("content does not contain %q:\n%s", substring, content)
	}
}

func requireNotContains(t *testing.T, content, substring string) {
	t.Helper()
	if strings.Contains(content, substring) {
		t.Fatalf("content contains %q:\n%s", substring, content)
	}
}
