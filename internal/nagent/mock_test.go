package nagent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockHandlerEchoesOpaquePayload(t *testing.T) {
	payload := "\x00\x01opaque"
	request := httptest.NewRequest(http.MethodPost,
		"/nagent-intent/v1/intent/imsi-001010000000001", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()

	NewMockHandler(MockConfig{}).ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != payload {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
}

func TestMockHandlerRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "method", method: http.MethodGet, path: "/nagent-intent/v1/intent/supi", body: `{}`, status: 405},
		{name: "path", method: http.MethodPost, path: "/wrong", body: `{}`, status: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/octet-stream")
			recorder := httptest.NewRecorder()
			NewMockHandler(MockConfig{}).ServeHTTP(recorder, request)
			if recorder.Code != tt.status {
				t.Fatalf("status=%d, want %d", recorder.Code, tt.status)
			}
		})
	}
}
