package connectclient

// wireError is the JSON body of a Connect protocol error:
//
//	{"code": "not_found", "message": "...", "details": [{"type": "...", "value": "...", "debug": {...}}]}
//
// decodeWireError decodes it. With Go 1.27 or later, it uses
// encoding/json/jsontext (wire_error_jsontext.go). With Go 1.26, which has
// jsontext only as an experiment, it uses encoding/json (wire_error_json.go).
// The reflection-based decoder in encoding/json adds about 130 KiB (Go 1.26)
// to 450 KiB (Go 1.27) to a stripped binary that does not use it already.
type wireError struct {
	code    string
	message string
	details []wireDetail
}

type wireDetail struct {
	typeName string
	value    string
	debug    []byte
}
