package controlplane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/controlplane"
)

func TestClientRequiresPrivateAuthenticatedTransport(t *testing.T) {
	token := strings.Repeat("t", 48)
	for _, in := range []struct {
		url, token string
		production bool
	}{
		{"http://example.com", token, false}, {"http://127.0.0.1", token, true}, {"https://example.com", "short", true}, {"https://user:password@example.com", token, true}, {"file:///tmp/authority", token, false}, {"https://example.com?token=x", token, true},
	} {
		if _, err := controlplane.New(in.url, in.token, in.production); err == nil {
			t.Fatalf("unsafe transport accepted: %s", in.url)
		}
	}
	for _, address := range []string{"http://127.0.0.1:3002", "https://authority.internal:3002"} {
		if _, err := controlplane.New(address, token, false); err != nil {
			t.Fatal(err)
		}
	}
}
func TestAuthorityErrorsFailClosedAndAreNotCached(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 48) {
			t.Error("service credential missing")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		status, body := 200, `{"Permission":"view_edit"}`
		if call > 1 {
			status, body = 503, `{"error":"private backend detail"}`
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	client, err := controlplane.New(server.URL, strings.Repeat("t", 48), false)
	if err != nil {
		t.Fatal(err)
	}
	if permission, err := client.EffectivePermission(context.Background(), 1, 2); err != nil || permission != access.ViewEdit {
		t.Fatal("initial authority response lost")
	}
	if permission, err := client.EffectivePermission(context.Background(), 1, 2); err != controlplane.ErrUnavailable || permission != access.NoAccess {
		t.Fatal("stale permission survived authority failure")
	}
	if calls.Load() != 2 {
		t.Fatal("permission was cached")
	}
}
func TestPrivateResponseNeverSerializesConnectionPasswordToPublicJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ID":2,"DBMS":"postgresql","Active":true,"Username":"target","Password":"private-password"}`)
	}))
	defer server.Close()
	client, err := controlplane.New(server.URL, strings.Repeat("t", 48), false)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := client.Connection(context.Background(), 1, 2, access.ViewOnly)
	if err != nil || metadata.Password != "private-password" {
		t.Fatal("connection secret lost on private transport")
	}
	raw, _ := json.Marshal(metadata)
	if strings.Contains(string(raw), "private-password") {
		t.Fatal("connection secret escaped private contract")
	}
}
