package nativecodec

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/AlexKris/sidervia/internal/strictjson"
)

const MaxJSONDepth = 64

type RequestInfo struct {
	Model  string
	Stream bool
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func InspectRequest(body []byte, modelRequired bool) (RequestInfo, error) {
	object, err := decodeObject(body)
	if err != nil {
		return RequestInfo{}, err
	}
	var info RequestInfo
	if raw, ok := object["model"]; ok {
		if err := json.Unmarshal(raw, &info.Model); err != nil || strings.TrimSpace(info.Model) == "" || len(info.Model) > 200 {
			return RequestInfo{}, &Error{Code: "invalid_request", Message: "model must be a non-empty string"}
		}
	}
	if modelRequired && info.Model == "" {
		return RequestInfo{}, &Error{Code: "invalid_request", Message: "model is required"}
	}
	if raw, ok := object["stream"]; ok {
		if err := json.Unmarshal(raw, &info.Stream); err != nil {
			return RequestInfo{}, &Error{Code: "invalid_request", Message: "stream must be a boolean"}
		}
	}
	return info, nil
}

func RewriteRequest(body []byte, allowed map[string]struct{}, upstreamModel string, requireModel bool) ([]byte, RequestInfo, error) {
	object, err := decodeObject(body)
	if err != nil {
		return nil, RequestInfo{}, err
	}
	unknown := make([]string, 0)
	for field := range object {
		if _, ok := allowed[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, RequestInfo{}, unknownField(unknown[0])
	}
	info, err := inspectObject(object, requireModel)
	if err != nil {
		return nil, RequestInfo{}, err
	}
	if requireModel {
		encoded, err := json.Marshal(upstreamModel)
		if err != nil {
			return nil, RequestInfo{}, err
		}
		object["model"] = encoded
	}
	rewritten, err := json.Marshal(object)
	if err != nil {
		return nil, RequestInfo{}, err
	}
	return rewritten, info, nil
}

func RewriteResponseModel(body []byte, publicModel string) ([]byte, error) {
	object, err := decodeObject(body)
	if err != nil {
		return nil, err
	}
	if _, ok := object["model"]; ok {
		encoded, err := json.Marshal(publicModel)
		if err != nil {
			return nil, err
		}
		object["model"] = encoded
	}
	return json.Marshal(object)
}

func RewriteProviderResponse(providerID string, body []byte, publicModel string) ([]byte, error) {
	if providerID == "google" {
		if err := strictjson.Validate(body, MaxJSONDepth); err != nil {
			return nil, &Error{Code: "upstream_protocol_changed", Message: err.Error()}
		}
		return append([]byte(nil), body...), nil
	}
	object, err := decodeObject(body)
	if err != nil {
		return nil, &Error{Code: "upstream_protocol_changed", Message: "upstream response is not valid JSON"}
	}
	encoded, err := json.Marshal(publicModel)
	if err != nil {
		return nil, err
	}
	if _, ok := object["model"]; ok {
		object["model"] = encoded
	}
	if providerID == "anthropic" {
		var message map[string]json.RawMessage
		if raw, ok := object["message"]; ok && json.Unmarshal(raw, &message) == nil {
			if _, exists := message["model"]; exists {
				message["model"] = encoded
				rewritten, marshalErr := json.Marshal(message)
				if marshalErr != nil {
					return nil, marshalErr
				}
				object["message"] = rewritten
			}
		}
	}
	return json.Marshal(object)
}

func decodeObject(body []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, &Error{Code: "invalid_request", Message: "request body is required"}
	}
	if err := strictjson.Validate(body, MaxJSONDepth); err != nil {
		return nil, &Error{Code: "invalid_request", Message: err.Error()}
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, &Error{Code: "invalid_request", Message: "request body must be a JSON object"}
	}
	return object, nil
}

func inspectObject(object map[string]json.RawMessage, requireModel bool) (RequestInfo, error) {
	var info RequestInfo
	if raw, ok := object["model"]; ok {
		if err := json.Unmarshal(raw, &info.Model); err != nil || strings.TrimSpace(info.Model) == "" || len(info.Model) > 200 {
			return RequestInfo{}, &Error{Code: "invalid_request", Message: "model must be a non-empty string"}
		}
	}
	if requireModel && info.Model == "" {
		return RequestInfo{}, &Error{Code: "invalid_request", Message: "model is required"}
	}
	if raw, ok := object["stream"]; ok {
		if err := json.Unmarshal(raw, &info.Stream); err != nil {
			return RequestInfo{}, &Error{Code: "invalid_request", Message: "stream must be a boolean"}
		}
	}
	return info, nil
}

func IsCode(err error, code string) bool {
	var codecError *Error
	return errors.As(err, &codecError) && codecError.Code == code
}
