//go:build go1.27

package connectclient

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"
)

// decodeWireError decodes a Connect error body with a jsontext.Decoder. It
// accepts what encoding/json would accept for the same struct: unknown and
// duplicate names, names in any case, null values, and invalid UTF-8.
func decodeWireError(data []byte) (*wireError, error) {
	decoder := jsontext.NewDecoder(
		bytes.NewReader(data),
		jsontext.AllowDuplicateNames(true),
		jsontext.AllowInvalidUTF8(true),
	)
	wire := &wireError{}
	err := decodeObject(decoder, func(name string) error {
		switch {
		case strings.EqualFold(name, "code"):
			return decodeString(decoder, &wire.code)
		case strings.EqualFold(name, "message"):
			return decodeString(decoder, &wire.message)
		case strings.EqualFold(name, "details"):
			return decodeDetails(decoder, &wire.details)
		default:
			return decoder.SkipValue()
		}
	})
	if err != nil {
		return nil, err
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the error object")
	}
	return wire, nil
}

func decodeDetails(decoder *jsontext.Decoder, details *[]wireDetail) error {
	switch decoder.PeekKind() {
	case 'n':
		_, err := decoder.ReadToken()
		return err
	case '[':
	default:
		return fmt.Errorf("details is a JSON %v, want an array", decoder.PeekKind())
	}
	if _, err := decoder.ReadToken(); err != nil {
		return err
	}
	for decoder.PeekKind() != ']' {
		var detail wireDetail
		err := decodeObject(decoder, func(name string) error {
			switch {
			case strings.EqualFold(name, "type"):
				return decodeString(decoder, &detail.typeName)
			case strings.EqualFold(name, "value"):
				return decodeString(decoder, &detail.value)
			case strings.EqualFold(name, "debug"):
				return decodeRaw(decoder, &detail.debug)
			default:
				return decoder.SkipValue()
			}
		})
		if err != nil {
			return err
		}
		*details = append(*details, detail)
	}
	_, err := decoder.ReadToken() // ']'
	return err
}

// decodeObject reads a JSON object and calls decodeMember for each name. The
// callback must read the value. A JSON null is an object with no members, as
// in encoding/json.
func decodeObject(decoder *jsontext.Decoder, decodeMember func(name string) error) error {
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if token.Kind() == 'n' {
		return nil
	}
	if token.Kind() != '{' {
		return fmt.Errorf("got a JSON %v, want an object", token.Kind())
	}
	for decoder.PeekKind() != '}' {
		name, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if err := decodeMember(name.String()); err != nil {
			return err
		}
	}
	_, err = decoder.ReadToken() // '}'
	return err
}

func decodeString(decoder *jsontext.Decoder, target *string) error {
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	switch token.Kind() {
	case '"':
		*target = token.String()
		return nil
	case 'n':
		return nil
	default:
		return fmt.Errorf("got a JSON %v, want a string", token.Kind())
	}
}

func decodeRaw(decoder *jsontext.Decoder, target *[]byte) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	if value.Kind() == 'n' {
		*target = nil
		return nil
	}
	// The decoder reuses its buffer, so copy the value.
	*target = bytes.Clone(value)
	return nil
}
