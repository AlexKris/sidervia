package usage

import (
	"bytes"
	"encoding/json"
	"strconv"
)

type Values map[string]int64

func (values Values) Merge(other Values) {
	for key, value := range other {
		if current, exists := values[key]; !exists || value > current {
			values[key] = value
		}
	}
}

func (values Values) JSON() string {
	payload := make(map[string]any, len(values)+1)
	payload["schema_version"] = 1
	for key, value := range values {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return `{"schema_version":1}`
	}
	return string(body)
}

func Extract(providerID string, body []byte) Values {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return Values{}
	}
	result := Values{}
	switch providerID {
	case "anthropic":
		usageObject(root["usage"], map[string]string{
			"input_tokens": "input_tokens", "output_tokens": "output_tokens",
			"cache_creation_input_tokens": "cache_write_tokens", "cache_read_input_tokens": "cache_read_tokens",
		}, result)
		if len(result) == 0 {
			var message map[string]json.RawMessage
			if json.Unmarshal(root["message"], &message) == nil {
				usageObject(message["usage"], map[string]string{
					"input_tokens": "input_tokens", "output_tokens": "output_tokens",
					"cache_creation_input_tokens": "cache_write_tokens", "cache_read_input_tokens": "cache_read_tokens",
				}, result)
			}
		}
	case "google":
		usageObject(root["usageMetadata"], map[string]string{
			"promptTokenCount": "input_tokens", "candidatesTokenCount": "output_tokens",
			"totalTokenCount": "total_tokens", "cachedContentTokenCount": "cache_read_tokens",
			"thoughtsTokenCount": "reasoning_tokens",
		}, result)
	default:
		usageObject(root["usage"], map[string]string{
			"prompt_tokens": "input_tokens", "completion_tokens": "output_tokens", "total_tokens": "total_tokens",
		}, result)
		var usage map[string]json.RawMessage
		if json.Unmarshal(root["usage"], &usage) == nil {
			usageObject(usage["prompt_tokens_details"], map[string]string{"cached_tokens": "cache_read_tokens"}, result)
			usageObject(usage["completion_tokens_details"], map[string]string{"reasoning_tokens": "reasoning_tokens"}, result)
		}
	}
	return result
}

func usageObject(raw json.RawMessage, fields map[string]string, destination Values) {
	if len(raw) == 0 || len(fields) == 0 {
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return
	}
	for source, target := range fields {
		decoder := json.NewDecoder(bytes.NewReader(object[source]))
		decoder.UseNumber()
		var raw any
		if decoder.Decode(&raw) != nil {
			continue
		}
		number, ok := raw.(json.Number)
		if !ok {
			continue
		}
		value, err := strconv.ParseInt(number.String(), 10, 64)
		if err == nil && value >= 0 {
			destination[target] = value
		}
	}
}
