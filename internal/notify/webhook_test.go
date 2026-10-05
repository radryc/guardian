package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookNotifierSendsSignedAlert(t *testing.T) {
	var received []byte
	var signature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = body
		signature = r.Header.Get("X-Guardian-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier("ops", server.URL, "topsecret", time.Second)
	alert := Alert{
		Event:     "partition.failing",
		Partition: "payments",
		Status:    "firing",
		Severity:  SeverityCritical,
		Summary:   "2 failing",
		FiredAt:   time.Now().UTC(),
	}
	if err := notifier.Send(context.Background(), alert); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var decoded Alert
	if err := json.Unmarshal(received, &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if decoded.Partition != "payments" || decoded.Event != "partition.failing" {
		t.Fatalf("decoded = %+v", decoded)
	}
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(received)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func TestWebhookNotifierRetries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier("ops", server.URL, "", time.Second)
	notifier.Retries = 3
	if err := notifier.Send(context.Background(), Alert{Partition: "payments", Event: "partition.failing"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}
