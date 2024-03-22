package plugin

import "html/template"

type methodTemplateData struct {
	// TypeName is the name of the service type. E.g., "UserService".
	TypeName string
	// MethodName is the name of the method. E.g., "GetUser".
	MethodName string
	// Request is the qualified name of the request message. E.g., "userv1.GetUserRequest".
	Request string
	// Response is the qualified name of the response message. E.g., "userv1.GetUserResponse".
	Response string
	// Procedure is the procedure to call. E.g., "/user.v1.UserService/GetUser".
	Procedure string
}

var methodTemplate = template.Must(template.New("method").Parse(`
func (s *{{ .TypeName }}) {{ .MethodName }}(ctx context.Context, req *{{ .Request }}) (*{{ .Response }}, error) {
	resp := new({{ .Response }})
	if err := s.client.do(ctx, req, resp, "{{ .Procedure }}"); err != nil {
		return nil, err
	}
	return resp, nil
}

`))
