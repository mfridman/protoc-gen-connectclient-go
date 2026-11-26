package connectclient

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

// jsonWireError is the struct that connecthttp decodes with encoding/json.
// decodeWireError must accept the same inputs and return the same values.
type jsonWireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Type  string          `json:"type"`
		Value string          `json:"value"`
		Debug json.RawMessage `json:"debug"`
	} `json:"details"`
}

var wireErrorInputs = []string{
	`{"code":"not_found","message":"no secret"}`,
	`{"code":"aborted","message":"m","details":[{"type":"google.protobuf.Duration","value":"CAM","debug":{"seconds":"3"}}]}`,
	`{"code":"internal","details":[{"type":"a","value":"AQ"},{"type":"b","value":"Ag","debug":"text"}]}`,
	`{"code":"unknown","extra":{"nested":[1,2,{"x":null}]},"message":"m"}`,
	`{"CODE":"unavailable","Message":"case"}`,
	`{"code":"a","code":"b"}`,
	`{"code":null,"message":null,"details":null}`,
	`{"details":[]}`,
	`{"details":[{"debug":null}]}`,
	"{\"message\":\"bad \xff utf8\"}",
	`{"message":"escaped é \n"}`,
	` { "code" : "ok" } `,
	`{}`,
	`null`,
	`{"details":[null]}`,
	// Invalid for both decoders.
	``,
	`[]`,
	`"text"`,
	`{"code":1}`,
	`{"message":true}`,
	`{"details":{}}`,
	`{"details":[1]}`,
	`{"details":[{"type":2}]}`,
	`{"code":"a"} trailing`,
	`{"code":"a"`,
	`{"code":}`,
	`{"code":"a",}`,
	`<html>error</html>`,
}

func TestDecodeWireErrorMatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	for _, input := range wireErrorInputs {
		checkWireErrorMatches(t, []byte(input))
	}
}

func FuzzDecodeWireError(f *testing.F) {
	for _, input := range wireErrorInputs {
		f.Add([]byte(input))
	}
	f.Fuzz(checkWireErrorMatches)
}

func checkWireErrorMatches(t *testing.T, data []byte) {
	t.Helper()
	var want jsonWireError
	wantErr := json.Unmarshal(data, &want)
	got, gotErr := decodeWireError(data)
	if (wantErr == nil) != (gotErr == nil) {
		t.Fatalf("input %q: encoding/json error %v, decodeWireError error %v", data, wantErr, gotErr)
	}
	if wantErr != nil {
		return
	}
	if got.code != want.Code || got.message != want.Message {
		t.Fatalf("input %q: got code %q message %q, want %q %q", data, got.code, got.message, want.Code, want.Message)
	}
	if len(got.details) != len(want.Details) {
		t.Fatalf("input %q: got %d details, want %d", data, len(got.details), len(want.Details))
	}
	for index, detail := range got.details {
		wantDetail := want.Details[index]
		wantDebug := []byte(wantDetail.Debug)
		if bytes.Equal(wantDebug, []byte("null")) {
			wantDebug = nil
		}
		if detail.typeName != wantDetail.Type || detail.value != wantDetail.Value ||
			!slices.Equal(compactJSON(detail.debug), compactJSON(wantDebug)) {
			t.Fatalf("input %q: detail %d = %+v, want %+v", data, index, detail, wantDetail)
		}
	}
}

func compactJSON(data []byte) []byte {
	if data == nil {
		return nil
	}
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, data); err != nil {
		return data
	}
	return buffer.Bytes()
}
