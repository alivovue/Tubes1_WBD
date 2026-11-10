package controlplane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"tubes1_wbd/backend/access"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClientRequiresPrivateAuthenticatedTransport(t *testing.T) {
	token := strings.Repeat("t", 48)
	for _, in := range []struct {
		url, token string
		production bool
	}{
		{"http://example.com", token, false}, {"http://127.0.0.1", token, true}, {"https://example.com", "short", true}, {"https://user:password@example.com", token, true}, {"file:///tmp/authority", token, false}, {"https://example.com?token=x", token, true},
	} {
		if _, err := New(in.url, in.token, in.production); err == nil {
			t.Fatalf("unsafe transport accepted: %s", in.url)
		}
	}
	for _, address := range []string{"http://127.0.0.1:3002", "https://authority.internal:3002"} {
		if _, err := New(address, token, false); err != nil {
			t.Fatal(err)
		}
	}
}
func TestAuthorityErrorsFailClosedAndAreNotCached(t *testing.T) {
	client, _ := New("http://127.0.0.1", strings.Repeat("t", 48), false)
	calls := 0
	client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 48) {
			t.Fatal("service credential missing")
		}
		status, body := 200, `{"Permission":"view_edit"}`
		if calls > 1 {
			status, body = 503, `{"error":"private backend detail"}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if permission, err := client.EffectivePermission(context.Background(), 1, 2); err != nil || permission != access.ViewEdit {
		t.Fatal("initial authority response lost")
	}
	if permission, err := client.EffectivePermission(context.Background(), 1, 2); err != ErrUnavailable || permission != access.NoAccess {
		t.Fatal("stale permission survived authority failure")
	}
	if calls != 2 {
		t.Fatal("permission was cached")
	}
}
func TestPrivateResponseNeverSerializesConnectionPasswordToPublicJSON(t *testing.T) {
	client, _ := New("http://127.0.0.1", strings.Repeat("t", 48), false)
	client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ID":2,"DBMS":"postgresql","Active":true,"Username":"target","Password":"private-password"}`))}, nil
	})
	metadata, err := client.Connection(context.Background(), 1, 2, access.ViewOnly)
	if err != nil || metadata.Password != "private-password" {
		t.Fatal("connection secret lost on private transport")
	}
	raw, _ := json.Marshal(metadata)
	if strings.Contains(string(raw), "private-password") {
		t.Fatal("connection secret escaped private contract")
	}
}
