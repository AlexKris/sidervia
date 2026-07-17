package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Validate(body []byte, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeValue(decoder, 0, maxDepth); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func consumeValue(decoder *json.Decoder, depth, maxDepth int) error {
	token, err := decoder.Token()
	if err != nil {
		return errors.New("request body is not valid JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= maxDepth {
		return errors.New("request body exceeds maximum JSON nesting depth")
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return errors.New("request body is not valid JSON")
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("request body is not valid JSON")
			}
			if _, exists := seen[key]; exists {
				return errors.New("request body contains duplicate object keys")
			}
			seen[key] = struct{}{}
			if err := consumeValue(decoder, depth+1, maxDepth); err != nil {
				return err
			}
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return errors.New("request body is not valid JSON")
		}
	case '[':
		for decoder.More() {
			if err := consumeValue(decoder, depth+1, maxDepth); err != nil {
				return err
			}
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return errors.New("request body is not valid JSON")
		}
	default:
		return errors.New("request body is not valid JSON")
	}
	return nil
}
