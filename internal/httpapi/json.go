package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const (
	maxAdminBody = 1 << 20
	maxJSONDepth = 64
)

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	writeJSON(w, status, errorEnvelope{Error: apiError{Code: code, Message: message, RequestID: requestID(r.Context()), Details: details}})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return errors.New("request body is too large or unreadable")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("request body is required")
	}
	if err := validateJSONShape(body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("request body contains invalid or unknown fields")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func validateJSONShape(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return errors.New("request body is not valid JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= maxJSONDepth {
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
			if err := consumeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return errors.New("request body is not valid JSON")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder, depth+1); err != nil {
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
