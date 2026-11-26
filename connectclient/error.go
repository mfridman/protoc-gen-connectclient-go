package connectclient

import (
	"net/http"

	"connectrpc.com/connect/v2"
)

// errorFromResponse converts a non-200 response with a JSON body into a
// remote *connect.Error. A body that is not a Connect error gets a code from
// the HTTP status.
func errorFromResponse(httpResponse *http.Response, data []byte) error {
	code := codeFromHTTPStatus(httpResponse.StatusCode)
	wire, err := decodeWireError(data)
	if err != nil {
		return connect.NewError(code, httpResponse.Status).WithRemote()
	}
	var wireCode connect.Code
	if wire.code != "" && wireCode.UnmarshalText([]byte(wire.code)) == nil {
		code = wireCode
	}
	connectError := connect.NewError(code, wire.message).WithRemote()
	for _, detail := range wire.details {
		value, err := connect.DecodeBinaryHeader(detail.value)
		if err != nil {
			// Drop a detail we cannot decode instead of hiding the code and
			// message, as connecthttp does.
			continue
		}
		connectError = connectError.WithDetail(&connect.ErrorDetail{
			Type:  detail.typeName,
			Value: value,
			Debug: detail.debug,
		})
	}
	return connectError
}

// codeFromHTTPStatus maps an HTTP status to a code, as the Connect protocol
// specifies for responses without an error body.
func codeFromHTTPStatus(status int) connect.Code {
	switch status {
	case http.StatusBadRequest:
		return connect.CodeInternal
	case http.StatusUnauthorized:
		return connect.CodeUnauthenticated
	case http.StatusForbidden:
		return connect.CodePermissionDenied
	case http.StatusNotFound:
		return connect.CodeUnimplemented
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return connect.CodeUnavailable
	default:
		return connect.CodeUnknown
	}
}
