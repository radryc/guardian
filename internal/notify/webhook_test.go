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
	"strconv"
	"testing"
	"time"
)

func TestWebhookNotifierSendsSignedAlert(t *testing.T) {
	var received []byte
	var signature string
	var signatureV2 string
	var timestamp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = body
		signature = r.Header.Get("X-Guardian-Signature")
		signatureV2 = r.Header.Get("X-Guardian-Signature-V2")
		timestamp = r.Header.Get("X-Guardian-Timestamp")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	before := time.Now().UTC().Unix()
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
	after := time.Now().UTC().Unix()
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
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		t.Fatalf("X-Guardian-Timestamp %q is not an int64: %v", timestamp, err)
	}
	if ts < before || ts > after {
		t.Fatalf("X-Guardian-Timestamp = %d, want in [%d, %d]", ts, before, after)
	}
	macV2 := hmac.New(sha256.New, []byte("topsecret"))
	macV2.Write([]byte(timestamp + "."))
	macV2.Write(received)
	wantV2 := "sha256=" + hex.EncodeToString(macV2.Sum(nil))
	if signatureV2 != wantV2 {
		t.Fatalf("signatureV2 = %q, want %q", signatureV2, wantV2)
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
