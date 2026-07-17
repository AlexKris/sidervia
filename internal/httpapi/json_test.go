package httpapi

import (
	"strings"
	"testing"
)

func TestValidateJSONShapeRejectsExcessiveDepth(t *testing.T) {
	valid := strings.Repeat("[", 64) + "null" + strings.Repeat("]", 64)
	if err := validateJSONShape([]byte(valid)); err != nil {
		t.Fatalf("maximum supported depth was rejected: %v", err)
	}

	tooDeep := "[" + valid + "]"
	if err := validateJSONShape([]byte(tooDeep)); err == nil {
		t.Fatal("JSON deeper than 64 containers was accepted")
	}
}
