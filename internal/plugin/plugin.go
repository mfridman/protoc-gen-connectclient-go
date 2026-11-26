// Package plugin generates client-only Go code for Connect services.
//
// For each .proto file with services, it writes a <file>.connectclient.go
// file with one client type per service. For each Go package, it also writes
// a <package>.connectclient.go file with a Client type that bundles the
// service clients of that package, and a copy of the clientruntime package
// that sends the calls. The generated code only depends on the standard
// library and google.golang.org/protobuf. Streaming methods are skipped.
package plugin

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"runtime/debug"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	// Name is the name of the plugin.
	Name = "protoc-gen-connectclient-go"

	generatedFilenameExtension = ".connectclient.go"
	defaultPackageSuffix       = "client"
	packageSuffixFlagName      = "package_suffix"

	contextPackage       = protogen.GoImportPath("context")
	runtimePackageClause = "package clientruntime\n"
	runtimeVersionLine   = `const generatorVersion = "devel"`

	commentWidth = 97 // leave room for "// "

	// Field numbers in FileDescriptorProto, used to copy file-level comments.
	protoSyntaxFieldNumber  = 12
	protoPackageFieldNumber = 2
)

// Generate runs the plugin on a CodeGeneratorRequest. Errors in the request,
// such as an unknown option, are returned in the response, as protoc expects.
func Generate(request *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	var flagSet flag.FlagSet
	packageSuffix := flagSet.String(packageSuffixFlagName, defaultPackageSuffix, "")
	protogenPlugin, err := protogen.Options{
		ParamFunc: setOption(&flagSet),
	}.New(request)
	if err != nil {
		return &pluginpb.CodeGeneratorResponse{Error: proto.String(err.Error())}, nil
	}
	protogenPlugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL) |
		uint64(pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
	protogenPlugin.SupportedEditionsMinimum = descriptorpb.Edition_EDITION_PROTO2
	protogenPlugin.SupportedEditionsMaximum = descriptorpb.Edition_EDITION_2024
	if err := generate(protogenPlugin, *packageSuffix); err != nil {
		protogenPlugin.Error(err)
	}
	return protogenPlugin.Response(), nil
}

// outputPackage is a Go package that the plugin writes code into.
type outputPackage struct {
	goImportPath  protogen.GoImportPath
	goPackageName protogen.GoPackageName
	directory     string
	protoPackage  protoreflect.FullName
	services      []*protogen.Service
	filenames     map[string]struct{}
}

// runtimeSource is the source of the clientruntime package, which every
// generated package gets a copy of.
//
//go:embed clientruntime/clientruntime.go
var runtimeSource string

func generate(protogenPlugin *protogen.Plugin, packageSuffix string) error {
	if packageSuffix != "" && !token.IsIdentifier(packageSuffix) {
		return fmt.Errorf("%s %q is not a valid Go identifier", packageSuffixFlagName, packageSuffix)
	}
	_, runtimeBody, _ := strings.Cut(runtimeSource, runtimePackageClause)
	runtimeBody = strings.Replace(runtimeBody, runtimeVersionLine,
		`const generatorVersion = "`+ReleaseVersion(buildVersion())+`"`, 1)
	runtimeNames, err := declaredNames(runtimeBody)
	if err != nil {
		return err
	}
	var outputPackages []*outputPackage
	outputPackagesByImportPath := make(map[protogen.GoImportPath]*outputPackage)
	for _, file := range protogenPlugin.Files {
		if !file.Generate {
			continue
		}
		services := unaryServices(file)
		if len(services) == 0 {
			continue
		}
		goImportPath, goPackageName, filenamePrefix := outputLocation(file, packageSuffix)
		pkg, ok := outputPackagesByImportPath[goImportPath]
		if !ok {
			pkg = &outputPackage{
				goImportPath:  goImportPath,
				goPackageName: goPackageName,
				directory:     path.Dir(filenamePrefix),
				protoPackage:  file.Desc.Package(),
				filenames:     make(map[string]struct{}),
			}
			outputPackagesByImportPath[goImportPath] = pkg
			outputPackages = append(outputPackages, pkg)
		}
		pkg.services = append(pkg.services, services...)
		filename := filenamePrefix + generatedFilenameExtension
		pkg.filenames[filename] = struct{}{}
		generatedFile := protogenPlugin.NewGeneratedFile(filename, goImportPath)
		if goImportPath != file.GoImportPath {
			generatedFile.Import(file.GoImportPath)
		}
		generateFile(generatedFile, file, goPackageName, services)
	}
	for _, pkg := range outputPackages {
		if err := checkCollisions(protogenPlugin, pkg, runtimeNames); err != nil {
			return err
		}
		filename := path.Join(pkg.directory, string(pkg.goPackageName)+generatedFilenameExtension)
		if _, ok := pkg.filenames[filename]; ok {
			return fmt.Errorf(
				"cannot write the Client type for package %s to %s: a .proto file generates the same name",
				pkg.protoPackage, filename,
			)
		}
		generatePackageClient(protogenPlugin.NewGeneratedFile(filename, pkg.goImportPath), pkg, runtimeBody)
	}
	return nil
}

// checkCollisions returns an error if a generated name is already declared
// by protoc-gen-go in the same Go package. This happens with an empty
// package_suffix, for example for a message named Client.
func checkCollisions(protogenPlugin *protogen.Plugin, pkg *outputPackage, runtimeNames []string) error {
	declared := make(map[string]struct{})
	for _, file := range protogenPlugin.Files {
		if file.GoImportPath != pkg.goImportPath {
			continue
		}
		declared[file.GoDescriptorIdent.GoName] = struct{}{}
		addEnumNames(declared, file.Enums)
		addMessageNames(declared, file.Messages)
		for _, extension := range file.Extensions {
			declared[extension.GoIdent.GoName] = struct{}{}
		}
	}
	if len(declared) == 0 {
		return nil
	}
	for _, name := range append(generatedNames(pkg), runtimeNames...) {
		if _, ok := declared[name]; ok {
			return fmt.Errorf(
				"generated name %s is already declared in Go package %s; set %s to a non-empty value",
				name, string(pkg.goImportPath), packageSuffixFlagName,
			)
		}
	}
	return nil
}

func addEnumNames(declared map[string]struct{}, enums []*protogen.Enum) {
	for _, enum := range enums {
		declared[enum.GoIdent.GoName] = struct{}{}
		for _, value := range enum.Values {
			declared[value.GoIdent.GoName] = struct{}{}
		}
	}
}

func addMessageNames(declared map[string]struct{}, messages []*protogen.Message) {
	for _, message := range messages {
		declared[message.GoIdent.GoName] = struct{}{}
		for _, field := range message.Fields {
			if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
				declared[field.GoIdent.GoName] = struct{}{}
			}
		}
		addEnumNames(declared, message.Enums)
		addMessageNames(declared, message.Messages)
	}
}

// generatedNames returns the package-level names that the plugin declares in
// pkg.
func generatedNames(pkg *outputPackage) []string {
	names := []string{"Client", "NewClient"}
	for _, service := range pkg.services {
		names = append(names, serviceClientName(service))
		for _, method := range unaryMethods(service) {
			names = append(names, procedureConstName(method))
		}
	}
	return names
}

// pseudoVersion matches the timestamp and commit at the end of a Go
// pseudo-version, such as v0.1.3-0.20261126180726-8ff1922f1dec.
var pseudoVersion = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)

// ReleaseVersion returns version if it is a released module version, such as
// "v0.2.0", and "devel" otherwise. Generated code gets the version, so local
// builds, which have a new pseudo-version for every commit, must not change
// it.
func ReleaseVersion(version string) string {
	if !strings.HasPrefix(version, "v") || strings.Contains(version, "+") || pseudoVersion.MatchString(version) {
		return "devel"
	}
	return version
}

// buildVersion returns the module version of the running program, such as
// "v0.2.0" for go install, a pseudo-version for a local build, or "(devel)".
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

// declaredNames returns the package-level names that source declares, after
// its package clause.
func declaredNames(source string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", "package clientruntime\n"+source, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse runtime source: %w", err)
	}
	var names []string
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil {
				names = append(names, decl.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, spec.Name.Name)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						names = append(names, name.Name)
					}
				}
			}
		}
	}
	return names, nil
}

// outputLocation returns where the generated code for file goes. With a
// package suffix, it goes into a sub-package of the .pb.go package, for
// example "apiv1" -> "apiv1client".
func outputLocation(
	file *protogen.File,
	packageSuffix string,
) (protogen.GoImportPath, protogen.GoPackageName, string) {
	if packageSuffix == "" {
		return file.GoImportPath, file.GoPackageName, file.GeneratedFilenamePrefix
	}
	goPackageName := file.GoPackageName + protogen.GoPackageName(packageSuffix)
	filenamePrefix := path.Join(
		path.Dir(file.GeneratedFilenamePrefix),
		string(goPackageName),
		path.Base(file.GeneratedFilenamePrefix),
	)
	goImportPath := protogen.GoImportPath(path.Join(string(file.GoImportPath), string(goPackageName)))
	return goImportPath, goPackageName, filenamePrefix
}

func generateFile(
	generatedFile *protogen.GeneratedFile,
	file *protogen.File,
	goPackageName protogen.GoPackageName,
	services []*protogen.Service,
) {
	generatePreamble(generatedFile, file, goPackageName)
	generateConstants(generatedFile, services)
	for _, service := range services {
		generateServiceClient(generatedFile, service)
	}
}

func generatePreamble(
	generatedFile *protogen.GeneratedFile,
	file *protogen.File,
	goPackageName protogen.GoPackageName,
) {
	syntaxLocation := file.Desc.SourceLocations().ByPath(protoreflect.SourcePath{protoSyntaxFieldNumber})
	for _, comment := range syntaxLocation.LeadingDetachedComments {
		writeLeadingComments(generatedFile, protogen.Comments(comment), false)
		generatedFile.P()
	}
	if syntaxLocation.LeadingComments != "" {
		writeLeadingComments(generatedFile, protogen.Comments(syntaxLocation.LeadingComments), false)
		generatedFile.P()
	}
	generatedFile.P("// Code generated by ", Name, ". DO NOT EDIT.")
	generatedFile.P("//")
	if file.Proto.GetOptions().GetDeprecated() {
		generatedFile.P("// Source: ", file.Desc.Path(), " is a deprecated file.")
	} else {
		generatedFile.P("// Source: ", file.Desc.Path())
	}
	generatedFile.P()
	packageLocation := file.Desc.SourceLocations().ByPath(protoreflect.SourcePath{protoPackageFieldNumber})
	for _, comment := range packageLocation.LeadingDetachedComments {
		writeLeadingComments(generatedFile, protogen.Comments(comment), false)
		generatedFile.P()
	}
	writeLeadingComments(generatedFile, protogen.Comments(packageLocation.LeadingComments), false)
	generatedFile.P("package ", goPackageName)
	generatedFile.P()
}

func generateConstants(generatedFile *protogen.GeneratedFile, services []*protogen.Service) {
	writeWrappedComment(generatedFile, "These constants are the procedure names of the unary RPCs in this file. ",
		"They are the paths of the HTTP requests.")
	generatedFile.P("const (")
	for _, service := range services {
		for _, method := range unaryMethods(service) {
			writeWrappedComment(generatedFile, procedureConstName(method), " is the procedure name of the ",
				service.Desc.Name(), "'s ", method.Desc.Name(), " RPC.")
			generatedFile.P(procedureConstName(method), ` = "/`, service.Desc.FullName(), "/", method.Desc.Name(), `"`)
		}
	}
	generatedFile.P(")")
	generatedFile.P()
}

func generateServiceClient(generatedFile *protogen.GeneratedFile, service *protogen.Service) {
	clientType := serviceClientName(service)
	writeWrappedComment(generatedFile, clientType, " is a client for the ", service.Desc.FullName(), " service.")
	if streaming := streamingMethodNames(service); len(streaming) > 0 {
		generatedFile.P("//")
		writeWrappedComment(generatedFile, "Streaming RPCs are not generated: ", strings.Join(streaming, ", "), ".")
	}
	if isDeprecatedService(service) {
		generatedFile.P("//")
		generatedFile.P("// Deprecated: do not use.")
	}
	generatedFile.P("type ", clientType, " struct {")
	generatedFile.P("conn *conn")
	generatedFile.P("}")
	generatedFile.P()
	for _, method := range unaryMethods(service) {
		writeLeadingComments(generatedFile, method.Comments.Leading, isDeprecatedMethod(method))
		generatedFile.P("func (c *", clientType, ") ", method.GoName,
			"(ctx ", contextPackage.Ident("Context"), ", req *", method.Input.GoIdent,
			") (*", method.Output.GoIdent, ", error) {")
		generatedFile.P("res := new(", method.Output.GoIdent, ")")
		generatedFile.P("if err := c.conn.call(ctx, ", procedureConstName(method), ", req, res); err != nil {")
		generatedFile.P("return nil, err")
		generatedFile.P("}")
		generatedFile.P("return res, nil")
		generatedFile.P("}")
		generatedFile.P()
	}
}

// generatePackageClient writes the Client type for pkg, followed by a copy of
// the runtime.
func generatePackageClient(generatedFile *protogen.GeneratedFile, pkg *outputPackage, runtimeBody string) {
	// The runtime body starts with its import block, which must come first.
	importEnd := strings.Index(runtimeBody, "\n)\n") + len("\n)\n")
	generatedFile.P("// Code generated by ", Name, ". DO NOT EDIT.")
	generatedFile.P()
	generatedFile.P("package ", pkg.goPackageName)
	generatedFile.P(runtimeBody[:importEnd])
	writeWrappedComment(generatedFile, "Client bundles a client for each service in the ", pkg.protoPackage,
		" package. The service clients share HTTP connections and options.")
	generatedFile.P("type Client struct {")
	for _, service := range pkg.services {
		generatedFile.P(service.GoName, " *", serviceClientName(service))
	}
	generatedFile.P("}")
	generatedFile.P()
	writeWrappedComment(generatedFile, "NewClient returns a Client for the services in the ", pkg.protoPackage,
		" package at baseURL, for example \"https://api.example.com\".")
	generatedFile.P("func NewClient(baseURL string, options ...Option) *Client {")
	generatedFile.P("conn := newConn(baseURL, options)")
	generatedFile.P("return &Client{")
	for _, service := range pkg.services {
		generatedFile.P(service.GoName, ": &", serviceClientName(service), "{conn: conn},")
	}
	generatedFile.P("}")
	generatedFile.P("}")
	generatedFile.P()
	writeWrappedComment(generatedFile, "The code below sends the calls. ", Name,
		" writes the same code into every package that it generates.")
	generatedFile.P(runtimeBody[importEnd:])
}

// setOption returns a protogen.Options.ParamFunc that sets plugin options on
// flagSet.
func setOption(flagSet *flag.FlagSet) func(name, value string) error {
	return func(name, value string) error {
		if flagSet.Lookup(name) == nil {
			if name == "separate_packages" {
				return fmt.Errorf("unknown option %q: use %s instead", name, packageSuffixFlagName)
			}
			return fmt.Errorf("unknown option %q", name)
		}
		return flagSet.Set(name, value)
	}
}

// unaryServices returns the services in file with at least one unary method.
func unaryServices(file *protogen.File) []*protogen.Service {
	var services []*protogen.Service
	for _, service := range file.Services {
		if len(unaryMethods(service)) > 0 {
			services = append(services, service)
		}
	}
	return services
}

func unaryMethods(service *protogen.Service) []*protogen.Method {
	var methods []*protogen.Method
	for _, method := range service.Methods {
		if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
			methods = append(methods, method)
		}
	}
	return methods
}

func streamingMethodNames(service *protogen.Service) []string {
	var names []string
	for _, method := range service.Methods {
		if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
			names = append(names, string(method.Desc.Name()))
		}
	}
	return names
}

func serviceClientName(service *protogen.Service) string {
	return service.GoName + "Client"
}

func procedureConstName(method *protogen.Method) string {
	return method.Parent.GoName + method.GoName + "Procedure"
}

func isDeprecatedService(service *protogen.Service) bool {
	serviceOptions, ok := service.Desc.Options().(*descriptorpb.ServiceOptions)
	return ok && serviceOptions.GetDeprecated()
}

func isDeprecatedMethod(method *protogen.Method) bool {
	methodOptions, ok := method.Desc.Options().(*descriptorpb.MethodOptions)
	return ok && methodOptions.GetDeprecated()
}

func writeLeadingComments(generatedFile *protogen.GeneratedFile, comments protogen.Comments, deprecated bool) {
	text := strings.TrimSpace(comments.String())
	if text != "" {
		generatedFile.P(text)
	}
	if deprecated {
		if text != "" {
			generatedFile.P("//")
		}
		generatedFile.P("// Deprecated: do not use.")
	}
}

// writeWrappedComment writes elems as a comment, wrapped at commentWidth.
func writeWrappedComment(generatedFile *protogen.GeneratedFile, elems ...any) {
	var text bytes.Buffer
	for _, elem := range elems {
		fmt.Fprint(&text, elem)
	}
	words := strings.Fields(text.String())
	text.Reset()
	var position int
	for _, word := range words {
		runeCount := utf8.RuneCountInString(word)
		if position > 0 && position+runeCount+1 > commentWidth {
			generatedFile.P("// ", text.String())
			text.Reset()
			position = 0
		}
		if position > 0 {
			text.WriteByte(' ')
			position++
		}
		text.WriteString(word)
		position += runeCount
	}
	if text.Len() > 0 {
		generatedFile.P("// ", text.String())
	}
}
