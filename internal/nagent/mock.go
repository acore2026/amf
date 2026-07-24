package nagent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type MockConfig struct {
	Delay           time.Duration
	Status          int
	MaxPayloadBytes int
	Logf            func(string, ...any)
}

func NewMockHandler(config MockConfig) http.Handler {
	if config.Status == 0 {
		config.Status = http.StatusOK
	}
	if config.MaxPayloadBytes <= 0 {
		config.MaxPayloadBytes = 65535
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		const prefix = "/nagent-intent/v1/intent/"
		if !strings.HasPrefix(request.URL.Path, prefix) || len(strings.TrimPrefix(request.URL.Path, prefix)) == 0 {
			http.NotFound(w, request)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, int64(config.MaxPayloadBytes)+1))
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}
		if len(body) > config.MaxPayloadBytes {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		if config.Delay > 0 {
			select {
			case <-request.Context().Done():
				return
			case <-time.After(config.Delay):
			}
		}
		if config.Logf != nil {
			supi := strings.TrimPrefix(request.URL.Path, prefix)
			hash := sha256.Sum256([]byte(supi))
			config.Logf("echo intent supiHash=%s payloadLength=%d idempotencyKey=%s",
				hex.EncodeToString(hash[:6]), len(body), request.Header.Get("Idempotency-Key"))
		}
		if config.Status != http.StatusOK {
			http.Error(w, fmt.Sprintf("configured status %d", config.Status), config.Status)
			return
		}
		contentType := request.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		for _, name := range []string{
			HeaderRequestID,
			HeaderAccessType,
			HeaderMessageIdentity,
			HeaderContainerType,
			HeaderPTI,
			HeaderPayloadID,
		} {
			if value := request.Header.Get(name); value != "" {
				w.Header().Set(name, value)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}
