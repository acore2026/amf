package gmm

import (
	stdcontext "context"
	"errors"
	"sync"
	"time"

	amf_context "github.com/acore2026/amf/internal/context"
	gmm_message "github.com/acore2026/amf/internal/gmm/message"
	"github.com/acore2026/amf/internal/nagent"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

const defaultNASTransportNAgentMaxPayloadBytes = 1400

var errNASTransportNAgentSubmitterUnavailable = errors.New("NAgent NAS Transport submitter is unavailable")

type dlNASTransportSender func(
	ranUe *amf_context.RanUe,
	payloadContainerType uint8,
	payload []byte,
	pduSessionID int32,
	cause uint8,
	backoffTimerUnit *uint8,
	backoffTimer uint8,
)

type nasTransportNAgentRuntimeConfig struct {
	Enabled              bool
	Generation           uint64
	PayloadContainerType uint8
	Submitter            nagent.Submitter
	RequestTimeout       time.Duration
	MaxPayloadBytes      int
	SendDLNASTransport   dlNASTransportSender
}

var nasTransportNAgentRuntime = struct {
	sync.RWMutex
	config         nasTransportNAgentRuntimeConfig
	nextGeneration uint64
}{}

func ConfigureNASTransportNAgentPassthrough(
	enabled bool,
	payloadContainerType uint8,
	submitter nagent.Submitter,
	requestTimeout time.Duration,
	maxPayloadBytes int,
) {
	configureNASTransportNAgentPassthroughRuntime(nasTransportNAgentRuntimeConfig{
		Enabled:              enabled,
		PayloadContainerType: payloadContainerType,
		Submitter:            submitter,
		RequestTimeout:       requestTimeout,
		MaxPayloadBytes:      maxPayloadBytes,
	})
}

func configureNASTransportNAgentPassthroughRuntime(config nasTransportNAgentRuntimeConfig) {
	if config.PayloadContainerType == 0 {
		config.PayloadContainerType = nasMessage.PayloadContainerTypeSOR
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 3 * time.Second
	}
	if config.MaxPayloadBytes <= 0 {
		config.MaxPayloadBytes = defaultNASTransportNAgentMaxPayloadBytes
	}
	if config.SendDLNASTransport == nil {
		config.SendDLNASTransport = gmm_message.SendDLNASTransport
	}
	nasTransportNAgentRuntime.Lock()
	nasTransportNAgentRuntime.nextGeneration++
	config.Generation = nasTransportNAgentRuntime.nextGeneration
	nasTransportNAgentRuntime.config = config
	nasTransportNAgentRuntime.Unlock()
}

func currentNASTransportNAgentRuntime() nasTransportNAgentRuntimeConfig {
	nasTransportNAgentRuntime.RLock()
	defer nasTransportNAgentRuntime.RUnlock()
	return nasTransportNAgentRuntime.config
}

func shouldHandleNASTransportNAgentPassthrough(payloadContainerType uint8) bool {
	runtime := currentNASTransportNAgentRuntime()
	return runtime.Enabled && payloadContainerType == runtime.PayloadContainerType
}

func handleULNASTransportNAgentPassthrough(
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	ulNasTransport *nasMessage.ULNASTransport,
) error {
	if ulNasTransport == nil {
		return errors.New("UL NAS Transport message is nil")
	}
	runtime := currentNASTransportNAgentRuntime()
	ranUe := ue.RanUeForAccessType(accessType)
	if ranUe == nil {
		return errors.New("RanUe is nil for NAS Transport NAgent passthrough")
	}
	requestPayload := ulNasTransport.PayloadContainer.GetPayloadContainerContents()
	responsePayload, err := processNASTransportNAgentPassthrough(ue, accessType, requestPayload)
	if err != nil {
		return err
	}
	runtime.SendDLNASTransport(
		ranUe,
		runtime.PayloadContainerType,
		responsePayload,
		0,
		0,
		nil,
		0,
	)
	return nil
}

func processNASTransportNAgentPassthrough(
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	requestPayload []byte,
) ([]byte, error) {
	runtime := currentNASTransportNAgentRuntime()
	if runtime.Submitter == nil {
		return nil, errNASTransportNAgentSubmitterUnavailable
	}
	if len(requestPayload) > runtime.MaxPayloadBytes {
		return buildAPIntentErrorPayload(nagent.ErrorCodePayloadTooLarge, false, 0), nil
	}

	ue.GmmLog.Infof("[NAgent NAS Transport] Sending passthrough payload: supi=%s accessType=%s payloadLength=%d",
		ue.Supi, string(accessType), len(requestPayload))
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), runtime.RequestTimeout)
	defer cancel()

	response, err := runtime.Submitter.SubmitIntent(ctx, nagent.IntentRequest{
		SUPI:       ue.Supi,
		AccessType: string(accessType),
		Payload:    append([]byte(nil), requestPayload...),
	})
	if err != nil {
		ue.GmmLog.Errorf("[NAgent NAS Transport] Received error response: err=%v", err)
		code := nagent.ErrorCodeUnavailable
		retryable := true
		httpStatus := 0
		var intentErr *nagent.Error
		if errors.As(err, &intentErr) {
			code = intentErr.Code
			retryable = intentErr.Retryable
			httpStatus = intentErr.HTTPStatus
		}
		return buildAPIntentErrorPayload(code, retryable, httpStatus), nil
	}
	if len(response) > runtime.MaxPayloadBytes {
		ue.GmmLog.Errorf("[NAgent NAS Transport] Response too large: responseLength=%d max=%d",
			len(response), runtime.MaxPayloadBytes)
		return buildAPIntentErrorPayload(nagent.ErrorCodeResponseTooLarge, false, 0), nil
	}
	ue.GmmLog.Infof("[NAgent NAS Transport] Received success response: responseLength=%d", len(response))
	return append([]byte(nil), response...), nil
}
