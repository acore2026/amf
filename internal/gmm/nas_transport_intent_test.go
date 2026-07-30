package gmm

import (
	stdcontext "context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	amf_context "github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/nagent"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

type captureTransportSubmitter struct {
	request  nagent.IntentRequest
	response []byte
	err      error
	calls    int
}

func (s *captureTransportSubmitter) SubmitIntent(
	ctx stdcontext.Context,
	request nagent.IntentRequest,
) ([]byte, error) {
	s.calls++
	s.request = request
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.response...), nil
}

func TestHandleULNASTransportType4PassthroughForwardsPayloadAndSendsResponse(t *testing.T) {
	submitter := &captureTransportSubmitter{response: []byte(`{"ok":true}`)}
	var sentType uint8
	var sentPayload []byte
	var sentRanUe *amf_context.RanUe
	configureNASTransportNAgentPassthroughRuntime(nasTransportNAgentRuntimeConfig{
		Enabled:              true,
		PayloadContainerType: nasMessage.PayloadContainerTypeSOR,
		Submitter:            submitter,
		RequestTimeout:       time.Second,
		MaxPayloadBytes:      1400,
		SendDLNASTransport: func(
			ranUe *amf_context.RanUe,
			payloadContainerType uint8,
			payload []byte,
			pduSessionID int32,
			cause uint8,
			backoffTimerUnit *uint8,
			backoffTimer uint8,
		) {
			sentRanUe = ranUe
			sentType = payloadContainerType
			sentPayload = append([]byte(nil), payload...)
		},
	})
	t.Cleanup(func() {
		ConfigureNASTransportNAgentPassthrough(false, nasMessage.PayloadContainerTypeSOR, nil, 0, 0)
	})

	ue, ranUe := testTransportUE("imsi-001010000000001")
	ul := testULNASTransport(nasMessage.PayloadContainerTypeSOR, []byte("hello"))

	if err := HandleULNASTransport(ue, models.AccessType__3_GPP_ACCESS, ul); err != nil {
		t.Fatalf("HandleULNASTransport() error = %v", err)
	}

	if submitter.calls != 1 {
		t.Fatalf("SubmitIntent calls = %d, want 1", submitter.calls)
	}
	if submitter.request.SUPI != ue.Supi ||
		submitter.request.AccessType != string(models.AccessType__3_GPP_ACCESS) ||
		string(submitter.request.Payload) != "hello" {
		t.Fatalf("SubmitIntent request = %#v", submitter.request)
	}
	if sentRanUe != ranUe {
		t.Fatalf("sent RanUe = %#v, want %#v", sentRanUe, ranUe)
	}
	if sentType != nasMessage.PayloadContainerTypeSOR {
		t.Fatalf("sent PayloadContainerType = %d, want %d", sentType, nasMessage.PayloadContainerTypeSOR)
	}
	if string(sentPayload) != `{"ok":true}` {
		t.Fatalf("sent payload = %s", sentPayload)
	}
}

func TestHandleULNASTransportType4PassthroughSendsErrorPayloadOnNAgentError(t *testing.T) {
	submitter := &captureTransportSubmitter{
		err: &nagent.Error{Code: nagent.ErrorCodeTimeout, Retryable: true},
	}
	var sentPayload []byte
	configureNASTransportNAgentPassthroughRuntime(nasTransportNAgentRuntimeConfig{
		Enabled:              true,
		PayloadContainerType: nasMessage.PayloadContainerTypeSOR,
		Submitter:            submitter,
		RequestTimeout:       time.Second,
		MaxPayloadBytes:      1400,
		SendDLNASTransport: func(
			ranUe *amf_context.RanUe,
			payloadContainerType uint8,
			payload []byte,
			pduSessionID int32,
			cause uint8,
			backoffTimerUnit *uint8,
			backoffTimer uint8,
		) {
			sentPayload = append([]byte(nil), payload...)
		},
	})
	t.Cleanup(func() {
		ConfigureNASTransportNAgentPassthrough(false, nasMessage.PayloadContainerTypeSOR, nil, 0, 0)
	})

	ue, _ := testTransportUE("imsi-001010000000001")
	ul := testULNASTransport(nasMessage.PayloadContainerTypeSOR, []byte("hello"))

	if err := HandleULNASTransport(ue, models.AccessType__3_GPP_ACCESS, ul); err != nil {
		t.Fatalf("HandleULNASTransport() error = %v", err)
	}
	if submitter.calls != 1 {
		t.Fatalf("SubmitIntent calls = %d, want 1", submitter.calls)
	}
	if string(sentPayload) == "" || !containsAll(string(sentPayload), "NAGENT_TIMEOUT", "error") {
		t.Fatalf("sent error payload = %s", sentPayload)
	}
}

func TestHandleULNASTransportType4PassthroughDisabledKeepsSORUnsupported(t *testing.T) {
	ConfigureNASTransportNAgentPassthrough(false, nasMessage.PayloadContainerTypeSOR, nil, 0, 0)

	ue, _ := testTransportUE("imsi-001010000000001")
	ul := testULNASTransport(nasMessage.PayloadContainerTypeSOR, []byte("hello"))

	err := HandleULNASTransport(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err == nil || !containsAll(err.Error(), "PayloadContainerTypeSOR", "not been implemented") {
		t.Fatalf("HandleULNASTransport() error = %v, want SOR unsupported", err)
	}
}

func TestProcessNASTransportNAgentPassthroughRejectsOversizedPayload(t *testing.T) {
	submitter := &captureTransportSubmitter{response: []byte("unused")}
	configureNASTransportNAgentPassthroughRuntime(nasTransportNAgentRuntimeConfig{
		Enabled:              true,
		PayloadContainerType: nasMessage.PayloadContainerTypeSOR,
		Submitter:            submitter,
		RequestTimeout:       time.Second,
		MaxPayloadBytes:      4,
	})
	t.Cleanup(func() {
		ConfigureNASTransportNAgentPassthrough(false, nasMessage.PayloadContainerTypeSOR, nil, 0, 0)
	})

	ue, _ := testTransportUE("imsi-001010000000001")
	payload, err := processNASTransportNAgentPassthrough(
		ue, models.AccessType__3_GPP_ACCESS, []byte("hello"))
	if err != nil {
		t.Fatalf("processNASTransportNAgentPassthrough() error = %v", err)
	}
	if submitter.calls != 0 {
		t.Fatalf("SubmitIntent calls = %d, want 0", submitter.calls)
	}
	if !containsAll(string(payload), nagent.ErrorCodePayloadTooLarge, "error") {
		t.Fatalf("payload = %s", payload)
	}
}

func TestProcessNASTransportNAgentPassthroughRequiresSubmitter(t *testing.T) {
	configureNASTransportNAgentPassthroughRuntime(nasTransportNAgentRuntimeConfig{
		Enabled:              true,
		PayloadContainerType: nasMessage.PayloadContainerTypeSOR,
		RequestTimeout:       time.Second,
		MaxPayloadBytes:      1400,
	})
	t.Cleanup(func() {
		ConfigureNASTransportNAgentPassthrough(false, nasMessage.PayloadContainerTypeSOR, nil, 0, 0)
	})

	ue, _ := testTransportUE("imsi-001010000000001")
	_, err := processNASTransportNAgentPassthrough(
		ue, models.AccessType__3_GPP_ACCESS, []byte("hello"))
	if err == nil || !errors.Is(err, errNASTransportNAgentSubmitterUnavailable) {
		t.Fatalf("processNASTransportNAgentPassthrough() error = %v", err)
	}
}

func testULNASTransport(payloadContainerType uint8, payload []byte) *nasMessage.ULNASTransport {
	ul := nasMessage.NewULNASTransport(0)
	ul.SpareHalfOctetAndPayloadContainerType.SetPayloadContainerType(payloadContainerType)
	ul.PayloadContainer.SetLen(uint16(len(payload)))
	ul.PayloadContainer.SetPayloadContainerContents(payload)
	return ul
}

func testTransportUE(supi string) (*amf_context.AmfUe, *amf_context.RanUe) {
	logger := logrus.New()
	logger.SetOutput(testingWriter{})
	ue := &amf_context.AmfUe{
		Supi:        supi,
		GmmLog:      logrus.NewEntry(logger),
		NASLog:      logrus.NewEntry(logger),
		ProducerLog: logrus.NewEntry(logger),
		RanUe:       make(map[models.AccessType]*amf_context.RanUe),
	}
	ranUe := &amf_context.RanUe{
		Ran: &amf_context.AmfRan{AnType: models.AccessType__3_GPP_ACCESS},
		Log: logrus.NewEntry(logger),
	}
	ue.AttachRanUe(ranUe)
	return ue, ranUe
}

type testingWriter struct{}

func (testingWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
