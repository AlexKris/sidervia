package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/store"
)

func TestExtractProviderUsage(t *testing.T) {
	tests := []struct {
		provider string
		body     string
		want     Values
	}{
		{"openai", `{"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}`, Values{"input_tokens": 10, "output_tokens": 4, "total_tokens": 14, "cache_read_tokens": 3, "reasoning_tokens": 2}},
		{"anthropic", `{"usage":{"input_tokens":9,"output_tokens":5,"cache_read_input_tokens":2}}`, Values{"input_tokens": 9, "output_tokens": 5, "cache_read_tokens": 2}},
		{"google", `{"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":6,"totalTokenCount":14}}`, Values{"input_tokens": 8, "output_tokens": 6, "total_tokens": 14}},
	}
	for _, test := range tests {
		got := Extract(test.provider, []byte(test.body))
		for key, want := range test.want {
			if got[key] != want {
				t.Errorf("provider=%s key=%s got=%d want=%d", test.provider, key, got[key], want)
			}
		}
	}
	if got := Extract("openai", []byte(`{"usage":{"prompt_tokens":-1}}`)); len(got) != 0 {
		t.Fatalf("negative usage accepted: %v", got)
	}
	if got := Extract("openai", []byte(`{"usage":{"prompt_tokens":"10","completion_tokens":{"value":4}}}`)); len(got) != 0 {
		t.Fatalf("non-numeric usage accepted: %v", got)
	}
}

func TestWriterFlushesRequestMetadata(t *testing.T) {
	database, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UnixMilli()
	result, err := database.DB().Exec(`INSERT INTO client_keys(
		public_id, name, prefix, secret_verifier, status, created_at_ms
	) VALUES('sdr_ckey_usage', 'usage', '12345678', 'verifier', 'active', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	writer := NewWriter(database.DB())
	if err := writer.Enqueue(context.Background(), Event{
		RequestID: "sdr_req_usage", ClientKeyID: clientKeyID, Protocol: "openai", EndpointKind: "chat_completions",
		PublicModelID: "model", StatusCode: 200, StartedAtMS: now, CompletedAtMS: now + 1,
		UsageJSON: `{"schema_version":1,"input_tokens":12,"output_tokens":4}`, RoutingJSON: `{"schema_version":1}`,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.DB().QueryRow("SELECT count(*) FROM request_records WHERE public_id='sdr_req_usage'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	reader := NewReader(database.DB())
	page, err := reader.ListRequests(context.Background(), 50, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("request page=%#v err=%v", page, err)
	}
	record := page.Items[0]
	if record.ID != "sdr_req_usage" || record.ClientKeyID != "sdr_ckey_usage" || record.Usage["input_tokens"] != 12 || record.DurationMS != 1 {
		t.Fatalf("request record=%#v", record)
	}
	loaded, err := reader.GetRequest(context.Background(), record.ID)
	if err != nil || loaded.ID != record.ID {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	summary, err := reader.Summary24Hours(context.Background(), time.UnixMilli(now+1))
	if err != nil || summary.Requests != 1 || summary.InputTokens != 12 || summary.OutputTokens != 4 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	if _, err := reader.ListRequests(context.Background(), 50, "invalid"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor error=%v", err)
	}
}
