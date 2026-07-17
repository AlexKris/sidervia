package egress

import (
	"context"
	"net/netip"
	"net/url"
	"testing"

	"github.com/AlexKris/sidervia/internal/routing"
)

type fixedResolver map[string][]netip.Addr

func (r fixedResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

func TestResolveRejectsAnyPrivateAnswer(t *testing.T) {
	manager := New(Options{Resolver: fixedResolver{
		"mixed.example": {netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("127.0.0.1")},
	}})
	if _, err := manager.resolveAllowed(context.Background(), "mixed.example", false); err == nil {
		t.Fatal("mixed public/private DNS response was accepted")
	}
	if addresses, err := manager.resolveAllowed(context.Background(), "mixed.example", true); err != nil || len(addresses) != 2 {
		t.Fatalf("explicit private-network policy addresses=%v err=%v", addresses, err)
	}
}

func TestBuildTargetDeduplicatesVersionPrefix(t *testing.T) {
	target, err := buildTarget("https://api.example.test/v1", "/v1/chat/completions", url.Values{"a": {"b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := target.String(); got != "https://api.example.test/v1/chat/completions?a=b" {
		t.Fatalf("target=%q", got)
	}
}

func TestClientCacheDoesNotGrowWhenOnlyCredentialVersionChanges(t *testing.T) {
	manager := New(Options{})
	first, err := manager.client(routing.Candidate{AccountInternalID: 1, AccountVersion: 1, UpstreamVersion: 1}, "api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.client(routing.Candidate{AccountInternalID: 1, AccountVersion: 2, UpstreamVersion: 1}, "api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(manager.clients) != 1 {
		t.Fatalf("credential refresh created a new transport: clients=%d", len(manager.clients))
	}
}
