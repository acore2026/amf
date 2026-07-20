package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/acore2026/amf/internal/nagent"
)

func TestEmbeddedNAgentMockServesAndShutsDown(t *testing.T) {
	mock, err := newEmbeddedNAgentMock(
		"127.0.0.1:0",
		nagent.NewMockHandler(nagent.MockConfig{}),
	)
	if err != nil {
		t.Fatalf("newEmbeddedNAgentMock() error = %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- mock.Serve() }()

	client := nagent.NewClient(nagent.ClientConfig{
		BaseURI:         "http://" + mock.Address(),
		ConnectTimeout:  time.Second,
		AttemptTimeout:  time.Second,
		TotalTimeout:    2 * time.Second,
		MaxAttempts:     1,
		MaxPayloadBytes: 65535,
	})
	request := nagent.IntentRequest{
		SUPI:            "imsi-001010000000001",
		AccessType:      "3GPP_ACCESS",
		MessageIdentity: 1,
		ContainerType:   0x0100,
		PTI:             5,
		PayloadID:       0x1234,
		Payload:         []byte(`{"intentDescription":"Locate the target UE"}`),
	}
	response, err := client.SubmitIntent(context.Background(), request)
	if err != nil {
		t.Fatalf("SubmitIntent() error = %v", err)
	}
	if !bytes.Equal(response, request.Payload) {
		t.Fatalf("response = %s, want %s", response, request.Payload)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mock.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("embedded NAgent mock did not stop")
	}
}
