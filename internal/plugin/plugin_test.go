package plugin

import (
	"go/parser"
	"go/token"
	gomaps "maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	testdataDir    = "../testdata"
	goldenDir      = "../integration/gen"
	testImportPath = "github.com/mfridman/protoc-gen-connectclient-go/internal/integration/gen"
)

// testFiles are the .proto files in internal/proto, with the Go packages
// that buf's managed mode gives them in internal/integration/buf.gen.yaml.
var testFiles = map[string]string{
	"admin/v1/admin.proto":        testImportPath + "/admin/v1;adminv1",
	"api/v1/repository.proto":     testImportPath + "/api/v1;apiv1",
	"api/v1/user_owner.proto":     testImportPath + "/api/v1;apiv1",
	"google/protobuf/empty.proto": "",
}

// TestGolden checks that the plugin generates the files committed in
// internal/integration/gen. Run "make proto" to update them.
func TestGolden(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(goldenDir); err != nil {
		// The golden files are in a nested module, which is not part of the
		// published module, for example when a user runs "go test all".
		t.Skipf("golden files not found: %v", err)
	}
	response := generateTest(t, "paths=source_relative")
	requireNoResponseError(t, response)
	var wantFiles []string
	err := filepath.WalkDir(goldenDir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, generatedFilenameExtension) {
			relativePath, err := filepath.Rel(goldenDir, path)
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
		want, err := os.ReadFile(filepath.Join(goldenDir, filepath.FromSlash(file.GetName())))
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
	requireContains(t, repository, "// Streaming RPCs are not generated: WatchRepository.")
	requireNotContains(t, repository, "WatchRepositoryProcedure")
	requireNotContains(t, repository, "EventService")
	requireContains(t, repository, "if err := c.conn.call(ctx, RepositoryServiceGetRepositoryProcedure, req, res); err != nil {")
	userOwner := got["api/v1/apiv1client/user_owner.connectclient.go"]
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

// TestSelfContained checks that generated code only imports the standard
// library, google.golang.org/protobuf, and the .pb.go packages.
func TestSelfContained(t *testing.T) {
	t.Parallel()
	response := generateTest(t, "paths=source_relative")
	requireNoResponseError(t, response)
	for _, file := range response.GetFile() {
		parsed, err := parser.ParseFile(token.NewFileSet(), file.GetName(), file.GetContent(), parser.ImportsOnly)
		requireNoError(t, err)
		for _, importSpec := range parsed.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			requireNoError(t, err)
			firstElement, _, _ := strings.Cut(importPath, "/")
			if strings.Contains(firstElement, ".") &&
				!strings.HasPrefix(importPath, "google.golang.org/protobuf/") &&
				!strings.HasPrefix(importPath, testImportPath+"/") {
				t.Errorf("%s imports %s", file.GetName(), importPath)
			}
		}
	}
	packageClient := responseFiles(response)["api/v1/apiv1client/apiv1client.connectclient.go"]
	requireContains(t, packageClient, "func (c *conn) call(ctx context.Context, procedure string, req, res proto.Message) error {")
	requireNotContains(t, packageClient, "package clientruntime")
}

func TestDeclaredNames(t *testing.T) {
	t.Parallel()
	_, body, ok := strings.Cut(runtimeSource, runtimePackageClause)
	if !ok {
		t.Fatalf("runtime source has no %q", runtimePackageClause)
	}
	names, err := declaredNames(body)
	requireNoError(t, err)
	for _, want := range []string{"Error", "ErrorDetail", "Option", "WithHTTPClient", "CodeNotFound", "conn", "newConn"} {
		if !slices.Contains(names, want) {
			t.Errorf("declared names %v do not contain %s", names, want)
		}
	}
}

func TestReleaseVersion(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]string{
		"v0.2.0":                               "v0.2.0",
		"v1.0.0-rc.1":                          "v1.0.0-rc.1",
		"(devel)":                              "devel",
		"":                                     "devel",
		"v0.2.0+dirty":                         "devel",
		"v0.1.3-0.20261126180726-8ff1922f1dec": "devel",
		"v0.1.3-0.20261126180726-8ff1922f1dec+dirty": "devel",
		"v0.0.0-20261126180726-8ff1922f1dec":         "devel",
		"v1.0.0-rc.1.0.20261126180726-8ff1922f1dec":  "devel",
	} {
		if got := ReleaseVersion(version); got != want {
			t.Errorf("ReleaseVersion(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestRuntimeVersionLine(t *testing.T) {
	t.Parallel()
	// The plugin replaces this line with the release version.
	if strings.Count(runtimeSource, runtimeVersionLine) != 1 {
		t.Fatalf("runtime source must contain %q once", runtimeVersionLine)
	}
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
		// rename renames services, by .proto file and service index.
		rename    map[string][]string
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
		{
			// HTTPClient and WithHTTPClient are declared by the runtime, and
			// the default package_suffix does not avoid them.
			name:      "runtime_name_collision",
			rename:    map[string][]string{"admin/v1/admin.proto": {"HTTP"}},
			wantError: "service admin.v1.HTTP generates the name HTTPClient, which the client runtime already declares",
		},
		{
			name:      "runtime_name_collision_with_option",
			rename:    map[string][]string{"admin/v1/admin.proto": {"WithHTTP"}},
			wantError: "service admin.v1.WithHTTP generates the name WithHTTPClient",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := newTestRequest(t, test.parameter)
			if test.files != nil {
				request.FileToGenerate = test.files
			}
			for _, file := range request.GetProtoFile() {
				for index, name := range test.rename[file.GetName()] {
					file.GetService()[index].Name = new(name)
				}
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
		Parameter:      new(strings.Join(parameters, ",")),
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
