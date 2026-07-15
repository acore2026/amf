package nagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientSubmitIntentEchoesJSONAndSetsHeaders(t *testing.T) {
	payload := []byte(`{"intent":"locate","target":"cell-1"}`)
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/nagent-intent/v1/intent/imsi-001010000000001" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		gotKey = r.Header.Get("Idempotency-Key")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	request := IntentRequest{
		SUPI:            "imsi-001010000000001",
		MessageIdentity: 1,
		ContainerType:   0x0100,
		PTI:             5,
		PayloadID:       0x1234,
		Payload:         payload,
	}
	response, err := client.SubmitIntent(context.Background(), request)
	if err != nil {
		t.Fatalf("SubmitIntent() error = %v", err)
	}
	if string(response) != string(payload) {
		t.Fatalf("response = %s, want %s", response, payload)
	}
	if len(gotKey) != 64 || gotKey != IdempotencyKey(request) {
		t.Fatalf("Idempotency-Key = %q", gotKey)
	}
}

func TestClientRetriesRetryableStatusWithStableKey(t *testing.T) {
	var attempts atomic.Int32
	var keysMu sync.Mutex
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keysMu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		keysMu.Unlock()
		if attempts.Add(1) < 3 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	response, err := client.SubmitIntent(context.Background(), validIntentRequest())
	if err != nil {
		t.Fatalf("SubmitIntent() error = %v", err)
	}
	if string(response) != `{"ok":true}` || attempts.Load() != 3 {
		t.Fatalf("response=%s attempts=%d", response, attempts.Load())
	}
	if keys[0] == "" || keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("idempotency keys changed across retries: %v", keys)
	}
}

func TestClientDoesNotRetryPermanentHTTPError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).SubmitIntent(context.Background(), validIntentRequest())
	assertIntentError(t, err, ErrorCodeRejected, false)
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}

func TestClientRejectsInvalidRequestBeforeHTTP(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		attempts.Add(1)
	}))
	defer server.Close()

	request := validIntentRequest()
	request.Payload = []byte(`{"broken"`)
	_, err := newTestClient(server.URL).SubmitIntent(context.Background(), request)
	assertIntentError(t, err, ErrorCodeInvalidJSON, false)
	if attempts.Load() != 0 {
		t.Fatalf("attempts = %d, want 0", attempts.Load())
	}
}

func TestClientRejectsInvalidOrOversizedResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "invalid JSON", body: `{"broken"`, code: ErrorCodeInvalidResponse},
		{name: "too large", body: `"` + strings.Repeat("a", 128) + `"`, code: ErrorCodeResponseTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client := newTestClient(server.URL)
			client.maxPayloadBytes = 64
			_, err := client.SubmitIntent(context.Background(), validIntentRequest())
			assertIntentError(t, err, tt.code, false)
		})
	}
}

func TestClientHonorsTotalTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"late":true}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	client.totalTimeout = 50 * time.Millisecond
	client.attemptTimeout = 40 * time.Millisecond
	_, err := client.SubmitIntent(context.Background(), validIntentRequest())
	assertIntentError(t, err, ErrorCodeTimeout, true)
}

func newTestClient(baseURI string) *Client {
	return NewClient(ClientConfig{
		BaseURI:            baseURI,
		ConnectTimeout:     time.Second,
		AttemptTimeout:     time.Second,
		TotalTimeout:       3 * time.Second,
		MaxAttempts:        3,
		MaxPayloadBytes:    65535,
		InitialBackoff:     time.Millisecond,
		MaxResponseBodyLog: 0,
	})
}

func validIntentRequest() IntentRequest {
	return IntentRequest{
		SUPI:            "imsi-001010000000001",
		MessageIdentity: 1,
		ContainerType:   0x0100,
		PTI:             5,
		PayloadID:       0x1234,
		Payload:         []byte(`{"intent":"locate"}`),
	}
}

func assertIntentError(t *testing.T, err error, code string, retryable bool) {
	t.Helper()
	var intentErr *Error
	if !errors.As(err, &intentErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if intentErr.Code != code || intentErr.Retryable != retryable {
		t.Fatalf("error = %#v, want code=%s retryable=%v", intentErr, code, retryable)
	}
}
