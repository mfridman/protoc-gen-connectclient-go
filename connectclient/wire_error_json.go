//go:build !go1.27

package connectclient

import (
	"encoding/json"
)

// decodeWireError decodes a Connect error body with encoding/json, the same
// way connecthttp does.
func decodeWireError(data []byte) (*wireError, error) {
	var wire struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Type  string          `json:"type"`
			Value string          `json:"value"`
			Debug json.RawMessage `json:"debug"`
		} `json:"details"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	result := &wireError{code: wire.Code, message: wire.Message}
	for _, detail := range wire.Details {
		debug := []byte(detail.Debug)
		if string(debug) == "null" {
			debug = nil
		}
		result.details = append(result.details, wireDetail{
			typeName: detail.Type,
			value:    detail.Value,
			debug:    debug,
		})
	}
	return result, nil
}
