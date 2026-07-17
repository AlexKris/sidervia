package nativecodec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

var (
	chatMessageFields       = fieldSet("content", "role")
	textBlockFields         = fieldSet("text", "type")
	geminiContentFields     = fieldSet("parts", "role")
	geminiPartFields        = fieldSet("text")
	geminiGenerationFields  = fieldSet("candidateCount", "maxOutputTokens", "stopSequences", "temperature", "topK", "topP")
	geminiSafetySettingKeys = fieldSet("category", "threshold")
)

// ValidateOpenAIChatTextRequest limits the v0.2 native chat path to text
// messages. Non-text features are introduced only with their own verified
// capability contract.
func ValidateOpenAIChatTextRequest(body []byte) error {
	object, err := decodeObject(body)
	if err != nil {
		return err
	}
	messages, err := requiredArray(object, "messages")
	if err != nil {
		return err
	}
	for index, raw := range messages {
		path := fmt.Sprintf("messages[%d]", index)
		message, err := rawObject(raw, path)
		if err != nil {
			return err
		}
		if err := rejectUnknownFields(message, chatMessageFields, path); err != nil {
			return err
		}
		role, err := requiredString(message, "role", path+".role")
		if err != nil {
			return err
		}
		switch role {
		case "developer", "system", "user", "assistant":
		default:
			return invalidField(path+".role", "is not supported by the v0.2 text endpoint")
		}
		if err := validateTextContent(message["content"], path+".content"); err != nil {
			return err
		}
	}
	for _, field := range []string{"frequency_penalty", "presence_penalty", "temperature", "top_p"} {
		if err := validateOptionalNumber(object, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"max_completion_tokens", "max_tokens", "n", "seed"} {
		if err := validateOptionalInteger(object, field); err != nil {
			return err
		}
	}
	if err := validateOptionalBoolean(object, "stream"); err != nil {
		return err
	}
	if err := validateOptionalStringOrStringArray(object, "stop"); err != nil {
		return err
	}
	return nil
}

// ValidateAnthropicTextRequest limits the v0.2 Messages path to text blocks.
func ValidateAnthropicTextRequest(body []byte) error {
	object, err := decodeObject(body)
	if err != nil {
		return err
	}
	messages, err := requiredArray(object, "messages")
	if err != nil {
		return err
	}
	for index, raw := range messages {
		path := fmt.Sprintf("messages[%d]", index)
		message, err := rawObject(raw, path)
		if err != nil {
			return err
		}
		if err := rejectUnknownFields(message, chatMessageFields, path); err != nil {
			return err
		}
		role, err := requiredString(message, "role", path+".role")
		if err != nil {
			return err
		}
		if role != "user" && role != "assistant" {
			return invalidField(path+".role", "must be user or assistant")
		}
		if err := validateTextContent(message["content"], path+".content"); err != nil {
			return err
		}
	}
	if raw, exists := object["system"]; exists {
		if err := validateTextContent(raw, "system"); err != nil {
			return err
		}
	}
	if err := validateRequiredInteger(object, "max_tokens"); err != nil {
		return err
	}
	for _, field := range []string{"temperature", "top_p"} {
		if err := validateOptionalNumber(object, field); err != nil {
			return err
		}
	}
	if err := validateOptionalInteger(object, "top_k"); err != nil {
		return err
	}
	if err := validateOptionalBoolean(object, "stream"); err != nil {
		return err
	}
	if err := validateOptionalStringArray(object, "stop_sequences"); err != nil {
		return err
	}
	return nil
}

// ValidateGeminiTextRequest limits GenerateContent to text Parts and the
// documented v0.2 generation and safety controls.
func ValidateGeminiTextRequest(body []byte) error {
	object, err := decodeObject(body)
	if err != nil {
		return err
	}
	contents, err := requiredArray(object, "contents")
	if err != nil {
		return err
	}
	for index, raw := range contents {
		if err := validateGeminiContent(raw, fmt.Sprintf("contents[%d]", index)); err != nil {
			return err
		}
	}
	if raw, exists := object["systemInstruction"]; exists {
		if err := validateGeminiContent(raw, "systemInstruction"); err != nil {
			return err
		}
	}
	if raw, exists := object["generationConfig"]; exists {
		config, err := rawObject(raw, "generationConfig")
		if err != nil {
			return err
		}
		if err := rejectUnknownFields(config, geminiGenerationFields, "generationConfig"); err != nil {
			return err
		}
		for _, field := range []string{"candidateCount", "maxOutputTokens", "topK"} {
			if err := validateOptionalInteger(config, field); err != nil {
				return err
			}
		}
		for _, field := range []string{"temperature", "topP"} {
			if err := validateOptionalNumber(config, field); err != nil {
				return err
			}
		}
		if err := validateOptionalStringArray(config, "stopSequences"); err != nil {
			return err
		}
	}
	if raw, exists := object["safetySettings"]; exists {
		settings, err := rawArray(raw, "safetySettings")
		if err != nil {
			return err
		}
		for index, settingRaw := range settings {
			path := fmt.Sprintf("safetySettings[%d]", index)
			setting, err := rawObject(settingRaw, path)
			if err != nil {
				return err
			}
			if err := rejectUnknownFields(setting, geminiSafetySettingKeys, path); err != nil {
				return err
			}
			if _, err := requiredString(setting, "category", path+".category"); err != nil {
				return err
			}
			if _, err := requiredString(setting, "threshold", path+".threshold"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGeminiContent(raw json.RawMessage, path string) error {
	content, err := rawObject(raw, path)
	if err != nil {
		return err
	}
	if err := rejectUnknownFields(content, geminiContentFields, path); err != nil {
		return err
	}
	if rawRole, exists := content["role"]; exists {
		role, err := rawString(rawRole, path+".role")
		if err != nil {
			return err
		}
		if role != "user" && role != "model" {
			return invalidField(path+".role", "must be user or model")
		}
	}
	parts, err := requiredArray(content, "parts")
	if err != nil {
		return invalidField(path+".parts", "must be an array")
	}
	for index, partRaw := range parts {
		partPath := fmt.Sprintf("%s.parts[%d]", path, index)
		part, err := rawObject(partRaw, partPath)
		if err != nil {
			return err
		}
		if err := rejectUnknownFields(part, geminiPartFields, partPath); err != nil {
			return err
		}
		if _, err := requiredString(part, "text", partPath+".text"); err != nil {
			return err
		}
	}
	return nil
}

func validateTextContent(raw json.RawMessage, path string) error {
	if len(raw) == 0 {
		return invalidField(path, "is required")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return invalidField(path, "is required")
	}
	if trimmed[0] == '"' {
		_, err := rawString(raw, path)
		return err
	}
	blocks, err := rawArray(raw, path)
	if err != nil {
		return invalidField(path, "must be a string or an array of text blocks")
	}
	for index, blockRaw := range blocks {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		block, err := rawObject(blockRaw, blockPath)
		if err != nil {
			return err
		}
		if err := rejectUnknownFields(block, textBlockFields, blockPath); err != nil {
			return err
		}
		kind, err := requiredString(block, "type", blockPath+".type")
		if err != nil {
			return err
		}
		if kind != "text" {
			return &Error{Code: "capability_not_supported", Message: fmt.Sprintf("request field %q requires a capability not available in v0.2", blockPath+".type")}
		}
		if _, err := requiredString(block, "text", blockPath+".text"); err != nil {
			return err
		}
	}
	return nil
}

func requiredArray(object map[string]json.RawMessage, field string) ([]json.RawMessage, error) {
	raw, exists := object[field]
	if !exists {
		return nil, invalidField(field, "is required")
	}
	return rawArray(raw, field)
}

func rawArray(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, invalidField(path, "must be an array")
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, invalidField(path, "must be an array")
	}
	return values, nil
}

func rawObject(raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, invalidField(path, "must be an object")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, invalidField(path, "must be an object")
	}
	return object, nil
}

func requiredString(object map[string]json.RawMessage, field, path string) (string, error) {
	raw, exists := object[field]
	if !exists {
		return "", invalidField(path, "is required")
	}
	return rawString(raw, path)
}

func rawString(raw json.RawMessage, path string) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", invalidField(path, "must be a string")
	}
	return value, nil
}

func validateOptionalNumber(object map[string]json.RawMessage, field string) error {
	raw, exists := object[field]
	if !exists || isJSONNull(raw) {
		return nil
	}
	value, ok := decodeNumber(raw)
	if !ok {
		return invalidField(field, "must be a number")
	}
	parsed, err := strconv.ParseFloat(value.String(), 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return invalidField(field, "must be a finite number")
	}
	return nil
}

func validateOptionalInteger(object map[string]json.RawMessage, field string) error {
	raw, exists := object[field]
	if !exists || isJSONNull(raw) {
		return nil
	}
	return validateInteger(raw, field)
}

func validateRequiredInteger(object map[string]json.RawMessage, field string) error {
	raw, exists := object[field]
	if !exists || isJSONNull(raw) {
		return invalidField(field, "is required")
	}
	return validateInteger(raw, field)
}

func validateInteger(raw json.RawMessage, path string) error {
	value, ok := decodeNumber(raw)
	if !ok {
		return invalidField(path, "must be an integer")
	}
	if _, err := strconv.ParseInt(value.String(), 10, 64); err != nil {
		return invalidField(path, "must be an integer")
	}
	return nil
}

func decodeNumber(raw json.RawMessage) (json.Number, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	number, ok := value.(json.Number)
	return number, ok
}

func validateOptionalBoolean(object map[string]json.RawMessage, field string) error {
	raw, exists := object[field]
	if !exists || isJSONNull(raw) {
		return nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return invalidField(field, "must be a boolean")
	}
	return nil
}

func validateOptionalStringOrStringArray(object map[string]json.RawMessage, field string) error {
	raw, exists := object[field]
	if !exists || isJSONNull(raw) {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		_, err := rawString(raw, field)
		return err
	}
	return validateStringArray(raw, field)
}

func validateOptionalStringArray(object map[string]json.RawMessage, field string) error {
	raw, exists := object[field]
	if !exists || isJSONNull(raw) {
		return nil
	}
	return validateStringArray(raw, field)
}

func validateStringArray(raw json.RawMessage, path string) error {
	values, err := rawArray(raw, path)
	if err != nil {
		return invalidField(path, "must be an array of strings")
	}
	for index, value := range values {
		if _, err := rawString(value, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func rejectUnknownFields(object map[string]json.RawMessage, allowed map[string]struct{}, path string) error {
	unknown := make([]string, 0)
	for field := range object {
		if _, exists := allowed[field]; !exists {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	fieldPath := unknown[0]
	if path != "" {
		fieldPath = path + "." + fieldPath
	}
	return unknownField(fieldPath)
}

func unknownField(path string) error {
	return &Error{Code: "unknown_request_field", Message: fmt.Sprintf("request field %q is not verified for this provider endpoint", path)}
}

func invalidField(path, reason string) error {
	return &Error{Code: "invalid_request", Message: fmt.Sprintf("request field %q %s", path, reason)}
}

func fieldSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
