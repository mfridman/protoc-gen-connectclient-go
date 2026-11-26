// Package plugin generates client-only Go code for Connect services.
//
// For each .proto file with services, it writes a <file>.connectclient.go
// file with one client type per service. For each Go package, it also writes
// a <package>.connectclient.go file with a Client type that bundles the
// service clients of that package.
//
// The generated code calls connect.Client.CallUnary from connect-go v2, so it
// works with any connect.Transport. Streaming methods are skipped.
package plugin

import (
	"bytes"
	"flag"
	"fmt"
	"go/token"
	"path"
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

	// connectPackage is rewritten to connectV2Package by importRewriteFunc, so
	// that protogen names the import "connect" and not "v2".
	connectPackage   = protogen.GoImportPath("connectrpc.com/connect")
	connectV2Package = protogen.GoImportPath("connectrpc.com/connect/v2")
	contextPackage   = protogen.GoImportPath("context")
	syncPackage      = protogen.GoImportPath("sync")

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
		ParamFunc:         setOption(&flagSet),
		ImportRewriteFunc: importRewriteFunc,
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

func generate(protogenPlugin *protogen.Plugin, packageSuffix string) error {
	if packageSuffix != "" && !token.IsIdentifier(packageSuffix) {
		return fmt.Errorf("%s %q is not a valid Go identifier", packageSuffixFlagName, packageSuffix)
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
		if err := checkCollisions(protogenPlugin, pkg); err != nil {
			return err
		}
		filename := path.Join(pkg.directory, string(pkg.goPackageName)+generatedFilenameExtension)
		if _, ok := pkg.filenames[filename]; ok {
			return fmt.Errorf(
				"cannot write the Client type for package %s to %s: a .proto file generates the same name",
				pkg.protoPackage, filename,
			)
		}
		generatePackageClient(protogenPlugin.NewGeneratedFile(filename, pkg.goImportPath), pkg)
	}
	return nil
}

// checkCollisions returns an error if a generated name is already declared
// by protoc-gen-go in the same Go package. This happens with an empty
// package_suffix, for example for a message named Client.
func checkCollisions(protogenPlugin *protogen.Plugin, pkg *outputPackage) error {
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
	for _, name := range generatedNames(pkg) {
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
		names = append(names,
			serviceClientName(service),
			"New"+serviceClientName(service),
			serviceNameConstName(service),
		)
		for _, method := range unaryMethods(service) {
			names = append(names, procedureConstName(method), specVarName(method))
		}
	}
	return names
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
		generateSpecVars(generatedFile, file, service)
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
	generatedFile.P("const (")
	for _, service := range services {
		constName := serviceNameConstName(service)
		writeWrappedComment(generatedFile, constName, " is the fully-qualified name of the ",
			service.Desc.Name(), " service.")
		generatedFile.P(constName, ` = "`, service.Desc.FullName(), `"`)
	}
	generatedFile.P(")")
	generatedFile.P()
	writeWrappedComment(generatedFile, "These constants are the procedure names of the unary RPCs in this file. ",
		"They are the paths of the HTTP requests, and connect.Spec.Procedure at run time.")
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

// generateSpecVars writes one lazily built connect.Spec per method. Its
// Schema is the method descriptor from the .pb.go package, which
// interceptors such as protovalidate use.
func generateSpecVars(generatedFile *protogen.GeneratedFile, file *protogen.File, service *protogen.Service) {
	specType := generatedFile.QualifiedGoIdent(connectPackage.Ident("Spec"))
	onceValue := generatedFile.QualifiedGoIdent(syncPackage.Ident("OnceValue"))
	fileDescriptor := generatedFile.QualifiedGoIdent(file.GoDescriptorIdent)
	generatedFile.P("var (")
	for _, method := range unaryMethods(service) {
		generatedFile.P(specVarName(method), " = ", onceValue, "(func() ", specType, " {")
		generatedFile.P("return ", specType, "{")
		generatedFile.P("StreamType: ", connectPackage.Ident("StreamTypeUnary"), ",")
		generatedFile.P("Schema: ", fileDescriptor, `.Services().ByName("`, service.Desc.Name(),
			`").Methods().ByName("`, method.Desc.Name(), `"),`)
		generatedFile.P("Procedure: ", procedureConstName(method), ",")
		if idempotencyLevel := idempotencyLevelIdent(method); idempotencyLevel != "" {
			generatedFile.P("IdempotencyLevel: ", connectPackage.Ident(idempotencyLevel), ",")
		}
		generatedFile.P("}")
		generatedFile.P("})")
	}
	generatedFile.P(")")
	generatedFile.P()
}

func generateServiceClient(generatedFile *protogen.GeneratedFile, service *protogen.Service) {
	clientType := serviceClientName(service)
	connectClient := connectPackage.Ident("Client")
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
	generatedFile.P("client *", connectClient)
	generatedFile.P("}")
	generatedFile.P()
	writeWrappedComment(generatedFile, "New", clientType, " returns a client for the ", service.Desc.FullName(),
		" service. Service clients may share one connect.Client.")
	if isDeprecatedService(service) {
		generatedFile.P("//")
		generatedFile.P("// Deprecated: do not use.")
	}
	generatedFile.P("func New", clientType, "(client *", connectClient, ") *", clientType, " {")
	generatedFile.P("return &", clientType, "{client: client}")
	generatedFile.P("}")
	generatedFile.P()
	for _, method := range unaryMethods(service) {
		writeLeadingComments(generatedFile, method.Comments.Leading, isDeprecatedMethod(method))
		generatedFile.P("func (c *", clientType, ") ", method.GoName,
			"(ctx ", contextPackage.Ident("Context"), ", req *", method.Input.GoIdent,
			") (*", method.Output.GoIdent, ", error) {")
		generatedFile.P("var res ", method.Output.GoIdent)
		generatedFile.P("if err := c.client.CallUnary(ctx, ", specVarName(method), "(), req, &res); err != nil {")
		generatedFile.P("return nil, err")
		generatedFile.P("}")
		generatedFile.P("return &res, nil")
		generatedFile.P("}")
		generatedFile.P()
	}
}

func generatePackageClient(generatedFile *protogen.GeneratedFile, pkg *outputPackage) {
	connectClient := connectPackage.Ident("Client")
	generatedFile.P("// Code generated by ", Name, ". DO NOT EDIT.")
	generatedFile.P()
	generatedFile.P("package ", pkg.goPackageName)
	generatedFile.P()
	writeWrappedComment(generatedFile, "Client bundles a client for each service in the ", pkg.protoPackage,
		" package. The service clients share one connect.Client.")
	generatedFile.P("type Client struct {")
	for _, service := range pkg.services {
		generatedFile.P(service.GoName, " *", serviceClientName(service))
	}
	generatedFile.P("}")
	generatedFile.P()
	writeWrappedComment(generatedFile, "NewClient returns a Client for the services in the ", pkg.protoPackage,
		" package. To call a server with the Connect protocol, pass the result of connectclient.New.")
	generatedFile.P("func NewClient(client *", connectClient, ") *Client {")
	generatedFile.P("return &Client{")
	for _, service := range pkg.services {
		generatedFile.P(service.GoName, ": New", serviceClientName(service), "(client),")
	}
	generatedFile.P("}")
	generatedFile.P("}")
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

// importRewriteFunc names the connect-go v2 import "connect". See
// https://github.com/golang/protobuf/issues/1205.
func importRewriteFunc(importPath protogen.GoImportPath) protogen.GoImportPath {
	if importPath == connectPackage {
		return connectV2Package
	}
	return importPath
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

func serviceNameConstName(service *protogen.Service) string {
	return service.GoName + "Name"
}

func serviceClientName(service *protogen.Service) string {
	return service.GoName + "Client"
}

func procedureConstName(method *protogen.Method) string {
	return method.Parent.GoName + method.GoName + "Procedure"
}

func specVarName(method *protogen.Method) string {
	return unexport(method.Parent.GoName) + method.GoName + "Spec"
}

func idempotencyLevelIdent(method *protogen.Method) string {
	methodOptions, ok := method.Desc.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return ""
	}
	switch methodOptions.GetIdempotencyLevel() {
	case descriptorpb.MethodOptions_NO_SIDE_EFFECTS:
		return "IdempotencyNoSideEffects"
	case descriptorpb.MethodOptions_IDEMPOTENT:
		return "IdempotencyIdempotent"
	default:
		return ""
	}
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

// unexport lowercases the first letter of name. Go keywords get a leading
// underscore.
func unexport(name string) string {
	lowercased := strings.ToLower(name[:1]) + name[1:]
	if token.IsKeyword(lowercased) {
		return "_" + lowercased
	}
	return lowercased
}
