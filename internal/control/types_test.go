package control

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestOptionalReferencesAreOmittedFromJSON(t *testing.T) {
	for name, value := range map[string]any{
		"upstream": Upstream{},
		"account":  Account{},
	} {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(":null")) {
			t.Errorf("%s response contains an undocumented null: %s", name, body)
		}
	}
}
