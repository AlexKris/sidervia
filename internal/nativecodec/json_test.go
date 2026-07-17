package nativecodec

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRewriteRequestRejectsUnknownAndDuplicateFields(t *testing.T) {
	allowed := map[string]struct{}{"model": {}, "messages": {}, "stream": {}}
	for _, body := range []string{
		`{"model":"public","messages":[],"unknown":true}`,
		`{"model":"public","model":"other","messages":[]}`,
	} {
		if _, _, err := RewriteRequest([]byte(body), allowed, "upstream", true); err == nil {
			t.Fatalf("expected rejection for %s", body)
		}
	}

	rewritten, info, err := RewriteRequest([]byte(`{"model":"public","messages":[],"stream":true}`), allowed, "upstream", true)
	if err != nil || info.Model != "public" || !info.Stream || !bytes.Contains(rewritten, []byte(`"model":"upstream"`)) {
		t.Fatalf("rewritten=%s info=%+v err=%v", rewritten, info, err)
	}
}

func TestNativeResponsePreservesUnknownFields(t *testing.T) {
	rewritten, err := RewriteResponseModel([]byte(`{"model":"upstream","future":{"large":1234567890123456789}}`), "public")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rewritten, []byte(`"model":"public"`)) || !bytes.Contains(rewritten, []byte(`"future":{"large":1234567890123456789}`)) {
		t.Fatalf("unexpected response: %s", rewritten)
	}
}

func TestCopySSERetainsUnknownEventAndRewritesData(t *testing.T) {
	input := "event: future\ndata: {\"model\":\"upstream\",\"new_field\":true}\n\ndata: [DONE]\n\n"
	var output bytes.Buffer
	_, err := CopySSE(&output, strings.NewReader(input), SSEOptions{RewriteData: func(data []byte) ([]byte, error) {
		return RewriteResponseModel(data, "public")
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "event: future") || !strings.Contains(output.String(), `"model":"public"`) || !strings.Contains(output.String(), `"new_field":true`) || !strings.Contains(output.String(), "data: [DONE]") {
		t.Fatalf("unexpected SSE output: %q", output.String())
	}
}

func TestCopySSEStopsReadingOversizedUnterminatedEvent(t *testing.T) {
	source := &countingFillReader{remaining: MaxSSEEventBytes * 4}
	if _, err := CopySSE(io.Discard, source, SSEOptions{}); err == nil {
		t.Fatal("oversized SSE event was accepted")
	}
	if source.read > MaxSSEEventBytes+(64<<10) {
		t.Fatalf("reader consumed %d bytes before enforcing the %d-byte event limit", source.read, MaxSSEEventBytes)
	}
}

type countingFillReader struct {
	remaining int
	read      int
}

func (r *countingFillReader) Read(body []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if len(body) > r.remaining {
		body = body[:r.remaining]
	}
	for index := range body {
		body[index] = 'x'
	}
	r.remaining -= len(body)
	r.read += len(body)
	return len(body), nil
}
