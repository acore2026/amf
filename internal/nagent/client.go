package nagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ErrorCodeInvalidJSON      = "INVALID_JSON"
	ErrorCodeInvalidRequest   = "INVALID_REQUEST"
	ErrorCodePayloadTooLarge  = "PAYLOAD_TOO_LARGE"
	ErrorCodeTimeout          = "NAGENT_TIMEOUT"
	ErrorCodeUnavailable      = "NAGENT_UNAVAILABLE"
	ErrorCodeRejected         = "NAGENT_REJECTED"
	ErrorCodeInvalidResponse  = "NAGENT_INVALID_RESPONSE"
	ErrorCodeResponseTooLarge = "NAGENT_RESPONSE_TOO_LARGE"

	HeaderRequestID       = "X-NAgent-Request-ID"
	HeaderAccessType      = "X-AP-Access-Type"
	HeaderMessageIdentity = "X-AP-Message-Identity"
	HeaderContainerType   = "X-AP-Container-Type"
	HeaderPTI             = "X-AP-PTI"
	HeaderPayloadID       = "X-AP-Payload-ID"
)

type IntentRequest struct {
	SUPI            string
	AccessType      string
	MessageIdentity uint8
	ContainerType   uint16
	PTI             uint8
	PayloadID       uint16
	Payload         []byte
	Route           *AgentRoute
}

type ClientConfig struct {
	BaseURI            string
	PathTemplate       string
	ConnectTimeout     time.Duration
	AttemptTimeout     time.Duration
	TotalTimeout       time.Duration
	MaxAttempts        int
	MaxPayloadBytes    int
	InitialBackoff     time.Duration
	MaxResponseBodyLog int
}

type Error struct {
	Code       string
	Retryable  bool
	HTTPStatus int
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("%s (HTTP %d)", e.Code, e.HTTPStatus)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type Client struct {
	baseURI         string
	pathTemplate    string
	httpClient      *http.Client
	attemptTimeout  time.Duration
	totalTimeout    time.Duration
	maxAttempts     int
	maxPayloadBytes int
	initialBackoff  time.Duration
}

func NewClient(config ClientConfig) *Client {
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.PathTemplate == "" {
		config.PathTemplate = "/nagent-intent/v1/intent/{supi}"
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   config.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: config.AttemptTimeout,
	}
	return &Client{
		baseURI:         strings.TrimRight(config.BaseURI, "/"),
		pathTemplate:    config.PathTemplate,
		httpClient:      &http.Client{Transport: transport},
		attemptTimeout:  config.AttemptTimeout,
		totalTimeout:    config.TotalTimeout,
		maxAttempts:     config.MaxAttempts,
		maxPayloadBytes: config.MaxPayloadBytes,
		initialBackoff:  config.InitialBackoff,
	}
}

func (c *Client) resolvePath(request IntentRequest) string {
	path := c.pathTemplate
	path = strings.ReplaceAll(path, "{supi}", url.PathEscape(request.SUPI))
	return path
}

type Router struct {
	clients       map[string]*Client
	defaultClient *Client
}

func NewRouter(defaultClient *Client, routeClients map[string]*Client) *Router {
	return &Router{
		clients:       routeClients,
		defaultClient: defaultClient,
	}
}

func (r *Router) SubmitIntent(ctx context.Context, request IntentRequest) ([]byte, error) {
	client := r.defaultClient
	if request.Route != nil {
		if c, ok := r.clients[request.Route.Name]; ok {
			client = c
		}
	}
	if client == nil {
		return nil, &Error{Code: ErrorCodeUnavailable, Cause: fmt.Errorf("no client for route %q", request.Route.Name)}
	}
	return client.SubmitIntent(ctx, request)
}

func (c *Client) SubmitIntent(ctx context.Context, request IntentRequest) ([]byte, error) {
	if request.SUPI == "" {
		return nil, &Error{Code: ErrorCodeInvalidRequest}
	}
	if len(request.Payload) > c.maxPayloadBytes {
		return nil, &Error{Code: ErrorCodePayloadTooLarge}
	}
	if !json.Valid(request.Payload) {
		return nil, &Error{Code: ErrorCodeInvalidJSON}
	}
	totalCtx, cancel := context.WithTimeout(ctx, c.totalTimeout)
	defer cancel()

	key := IdempotencyKey(request)
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		response, err := c.submitAttempt(totalCtx, request, key)
		if err == nil {
			return response, nil
		}
		lastErr = err
		var intentErr *Error
		if !errors.As(err, &intentErr) || !intentErr.Retryable || attempt == c.maxAttempts {
			break
		}
		if err := waitBackoff(totalCtx, c.initialBackoff, attempt); err != nil {
			lastErr = &Error{Code: ErrorCodeTimeout, Retryable: true, Cause: err}
			break
		}
	}
	if totalCtx.Err() != nil {
		return nil, &Error{Code: ErrorCodeTimeout, Retryable: true, Cause: totalCtx.Err()}
	}
	return nil, lastErr
}

func (c *Client) submitAttempt(ctx context.Context, request IntentRequest, key string) ([]byte, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancel()
	endpoint := c.baseURI + c.resolvePath(request)
	httpRequest, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint,
		bytes.NewReader(request.Payload))
	if err != nil {
		return nil, &Error{Code: ErrorCodeInvalidRequest, Cause: err}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Idempotency-Key", key)
	httpRequest.Header.Set(HeaderRequestID, key)
	httpRequest.Header.Set(HeaderAccessType, request.AccessType)
	httpRequest.Header.Set(HeaderMessageIdentity, strconv.FormatUint(uint64(request.MessageIdentity), 10))
	httpRequest.Header.Set(HeaderContainerType, strconv.FormatUint(uint64(request.ContainerType), 10))
	httpRequest.Header.Set(HeaderPTI, strconv.FormatUint(uint64(request.PTI), 10))
	httpRequest.Header.Set(HeaderPayloadID, strconv.FormatUint(uint64(request.PayloadID), 10))

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{Code: ErrorCodeTimeout, Retryable: true, Cause: err}
		}
		return nil, &Error{Code: ErrorCodeUnavailable, Retryable: true, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		retryable := isRetryableStatus(response.StatusCode)
		code := ErrorCodeRejected
		if retryable {
			code = ErrorCodeUnavailable
		}
		return nil, &Error{Code: code, Retryable: retryable, HTTPStatus: response.StatusCode}
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return nil, &Error{Code: ErrorCodeInvalidResponse,
			Cause: fmt.Errorf("unexpected Content-Type %q", contentType)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(c.maxPayloadBytes)+1))
	if err != nil {
		return nil, &Error{Code: ErrorCodeInvalidResponse, Cause: err}
	}
	if len(body) > c.maxPayloadBytes {
		return nil, &Error{Code: ErrorCodeResponseTooLarge}
	}
	if !json.Valid(body) {
		return nil, &Error{Code: ErrorCodeInvalidResponse}
	}
	if err := validateResponseCorrelation(response.Header, request, key); err != nil {
		return nil, &Error{Code: ErrorCodeInvalidResponse, Cause: err}
	}
	return body, nil
}

func validateResponseCorrelation(header http.Header, request IntentRequest, requestID string) error {
	if got := header.Get(HeaderRequestID); got != "" && got != requestID {
		return fmt.Errorf("response request ID does not match request")
	}
	checks := []struct {
		name string
		bits int
		want uint64
	}{
		{name: HeaderMessageIdentity, bits: 8, want: uint64(request.MessageIdentity)},
		{name: HeaderContainerType, bits: 16, want: uint64(request.ContainerType)},
		{name: HeaderPTI, bits: 8, want: uint64(request.PTI)},
		{name: HeaderPayloadID, bits: 16, want: uint64(request.PayloadID)},
	}
	for _, check := range checks {
		value := header.Get(check.name)
		if value == "" {
			continue
		}
		got, err := strconv.ParseUint(value, 10, check.bits)
		if err != nil || got != check.want {
			return fmt.Errorf("response %s does not match request", check.name)
		}
	}
	if got := header.Get(HeaderAccessType); got != "" && got != request.AccessType {
		return fmt.Errorf("response %s does not match request", HeaderAccessType)
	}
	return nil
}

func IdempotencyKey(request IntentRequest) string {
	hash := IntentRequestFingerprint(request)
	return hex.EncodeToString(hash[:])
}

func IntentRequestFingerprint(request IntentRequest) [32]byte {
	h := sha256.New()
	_, _ = h.Write([]byte("nagent-intent-v1\x00"))
	writeLengthPrefixedString(h, request.SUPI)
	writeLengthPrefixedString(h, request.AccessType)
	_, _ = h.Write([]byte{request.MessageIdentity})
	var fields [5]byte
	binary.BigEndian.PutUint16(fields[0:2], request.ContainerType)
	fields[2] = request.PTI
	binary.BigEndian.PutUint16(fields[3:5], request.PayloadID)
	_, _ = h.Write(fields[:])
	payloadHash := sha256.Sum256(request.Payload)
	_, _ = h.Write(payloadHash[:])
	if request.Route != nil {
		writeLengthPrefixedString(h, request.Route.Name)
		writeLengthPrefixedString(h, request.Route.BaseURI)
		writeLengthPrefixedString(h, request.Route.Path)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeLengthPrefixedString(writer io.Writer, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func waitBackoff(ctx context.Context, initial time.Duration, attempt int) error {
	if initial <= 0 {
		return nil
	}
	delay := initial * time.Duration(1<<(attempt-1))
	jitter := 0.8 + rand.Float64()*0.4
	timer := time.NewTimer(time.Duration(float64(delay) * jitter))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r IntentRequest) String() string {
	return "supiHash=" + shortSUPIHash(r.SUPI) + " payloadId=" +
		strconv.FormatUint(uint64(r.PayloadID), 10) + " payloadLength=" + strconv.Itoa(len(r.Payload))
}

func shortSUPIHash(supi string) string {
	hash := sha256.Sum256([]byte(supi))
	return hex.EncodeToString(hash[:6])
}
