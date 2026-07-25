package gmm

import (
	stdcontext "context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	amf_context "github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/nagent"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

const (
	apIntentErrorPayloadIDConflict = "PAYLOAD_ID_CONFLICT"
	apIntentErrorQueueFull         = "AMF_QUEUE_FULL"
	apIntentResponseContainerType  = 0x0101
)

type apIntentRuntimeConfig struct {
	Enabled        bool
	Generation     uint64
	Submitter      nagent.Submitter
	RequestTimeout time.Duration
}

func ConfigureAPIntentIntegration(
	enabled bool,
	submitter nagent.Submitter,
	requestTimeout time.Duration,
) {
	configureAPIntentRuntime(apIntentRuntimeConfig{
		Enabled:        enabled,
		Submitter:      submitter,
		RequestTimeout: requestTimeout,
	})
}

var apIntentRuntime = struct {
	sync.RWMutex
	config         apIntentRuntimeConfig
	nextGeneration uint64
}{}

func configureAPIntentRuntime(config apIntentRuntimeConfig) {
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 3 * time.Second
	}
	apIntentRuntime.Lock()
	apIntentRuntime.nextGeneration++
	config.Generation = apIntentRuntime.nextGeneration
	apIntentRuntime.config = config
	apIntentRuntime.Unlock()
}

func currentAPIntentRuntime() apIntentRuntimeConfig {
	apIntentRuntime.RLock()
	defer apIntentRuntime.RUnlock()
	return apIntentRuntime.config
}

func apIntentIntegrationEnabled() bool {
	return currentAPIntentRuntime().Enabled
}

func processCompletedAPIntent(
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	messageIdentity uint8,
	complete *nasMessage.APContainer,
	ordinary []*nasMessage.CooperationIE,
) ([][]*nasMessage.CooperationIE, error) {
	runtime := currentAPIntentRuntime()
	if runtime.Submitter == nil {
		ue.GmmLog.Warn("[NAgent HTTP] Submitter is nil, storing completed container without NAgent call")
		ue.GetOrCreateCooperationContext().StoreCompletedAPContainer(amf_context.CompletedAPContainer{
			ContainerType:      complete.ContainerType,
			ContainerTypePTI:   complete.ContainerTypePTI,
			ContainerPayloadID: complete.ContainerPayloadID,
			Payload:            complete.Payload,
			CompletedAt:        time.Now(),
		})
		apResponses, err := buildDLAPContainerIEs(messageIdentity, complete)
		if err != nil {
			return groupOrdinaryDLCooperationIEs(ordinary), nil
		}
		return groupDLCooperationResponses(ordinary, apResponses), nil
	}

	requestForHTTP := nagent.IntentRequest{
		SUPI:            ue.Supi,
		AccessType:      string(accessType),
		MessageIdentity: messageIdentity,
		ContainerType:   complete.ContainerType,
		PTI:             complete.ContainerTypePTI,
		PayloadID:       complete.ContainerPayloadID,
		Payload:         append([]byte(nil), complete.Payload...),
	}

	ue.GmmLog.Infof("[NAgent HTTP] Sending intent to NAgent: supi=%s payloadId=0x%04x pti=0x%02x containerType=0x%04x accessType=%s payloadLength=%d payload=%s",
		ue.Supi, complete.ContainerPayloadID, complete.ContainerTypePTI, complete.ContainerType,
		string(accessType), len(complete.Payload), string(complete.Payload))

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), runtime.RequestTimeout)
	defer cancel()

	response, httpErr := runtime.Submitter.SubmitIntent(ctx, requestForHTTP)

	if httpErr != nil {
		ue.GmmLog.Errorf("[NAgent HTTP] Received error response: payloadId=0x%04x err=%v", complete.ContainerPayloadID, httpErr)
		code := nagent.ErrorCodeUnavailable
		retryable := true
		var intentErr *nagent.Error
		if errors.As(httpErr, &intentErr) {
			code = intentErr.Code
			retryable = intentErr.Retryable
		}
		return buildImmediateAPIntentError(ordinary, amf_context.APIntentRequest{
			PayloadID:        complete.ContainerPayloadID,
			MessageIdentity:  messageIdentity,
			AccessType:       accessType,
			ContainerType:    complete.ContainerType,
			ContainerTypePTI: complete.ContainerTypePTI,
			Payload:          complete.Payload,
		}, code, retryable, 0)
	}

	responsePreview := string(response)
	if len(responsePreview) > 256 {
		responsePreview = responsePreview[:256] + "..."
	}
	ue.GmmLog.Infof("[NAgent HTTP] Received success response: payloadId=0x%04x responseLength=%d response=%s",
		complete.ContainerPayloadID, len(response), responsePreview)

	container := &nasMessage.APContainer{
		ContainerType:      apIntentResponseContainerType,
		ContainerTypePTI:   complete.ContainerTypePTI,
		ContainerPayloadID: complete.ContainerPayloadID,
		Payload:            append([]byte(nil), response...),
	}

	ue.GmmLog.Infof("[DL AP Container] Delivering NAgent response: payloadId=0x%04x responsePayloadLength=%d",
		complete.ContainerPayloadID, len(response))

	apResponses, err := buildDLAPContainerIEs(messageIdentity, container)
	if err != nil {
		ue.GmmLog.Errorf("Build NAgent DL AP Container failed: %v", err)
		return groupOrdinaryDLCooperationIEs(ordinary), nil
	}

	for _, ie := range apResponses {
		if ie.GetIei() == nasMessage.CooperationIEType71 {
			frag, decErr := nasMessage.DecodeAPContainer(ie.GetContents())
			if decErr == nil {
				payloadPreview := string(frag.Payload)
				if len(payloadPreview) > 128 {
					payloadPreview = payloadPreview[:128] + "..."
				}
				ue.GmmLog.Infof("[DL AP Container] Fragment: payloadId=0x%04x DF=%v MF=%v offset=%d payloadLength=%d payload=%s",
					frag.ContainerPayloadID, frag.DontFragment(), frag.MoreFragments(),
					frag.FragmentOffset, len(frag.Payload), payloadPreview)
			}
		}
	}

	return groupDLCooperationResponses(ordinary, apResponses), nil
}

func buildImmediateAPIntentError(
	ordinary []*nasMessage.CooperationIE,
	request amf_context.APIntentRequest,
	code string,
	retryable bool,
	httpStatus int,
) ([][]*nasMessage.CooperationIE, error) {
	container := &nasMessage.APContainer{
		ContainerType:      apIntentResponseContainerType,
		ContainerTypePTI:   request.ContainerTypePTI,
		ContainerPayloadID: request.PayloadID,
		Payload:            buildAPIntentErrorPayload(code, retryable, httpStatus),
	}
	ies, err := buildDLAPContainerIEs(request.MessageIdentity, container)
	if err != nil {
		return nil, err
	}
	return groupDLCooperationResponses(ordinary, ies), nil
}

func buildAPIntentErrorPayload(code string, retryable bool, httpStatus int) []byte {
	payload := struct {
		NAgent struct {
			Version    int    `json:"version"`
			Status     string `json:"status"`
			Code       string `json:"code"`
			Retryable  bool   `json:"retryable"`
			HTTPStatus int    `json:"httpStatus"`
			Message    string `json:"message"`
		} `json:"$nagent"`
	}{}
	payload.NAgent.Version = 1
	payload.NAgent.Status = "error"
	payload.NAgent.Code = code
	payload.NAgent.Retryable = retryable
	payload.NAgent.HTTPStatus = httpStatus
	payload.NAgent.Message = apIntentPublicErrorMessage(code)
	encoded, _ := json.Marshal(payload)
	return encoded
}

func apIntentPublicErrorMessage(code string) string {
	switch code {
	case nagent.ErrorCodeInvalidJSON:
		return "The intent payload is not valid JSON"
	case nagent.ErrorCodeInvalidRequest:
		return "The intent request is invalid"
	case nagent.ErrorCodePayloadTooLarge:
		return "The intent payload is too large"
	case apIntentErrorPayloadIDConflict:
		return "The payload identifier is already in use"
	case apIntentErrorQueueFull:
		return "The AMF intent queue is full"
	case nagent.ErrorCodeTimeout:
		return "NAgent did not respond before the deadline"
	case nagent.ErrorCodeRejected:
		return "NAgent rejected the intent"
	case nagent.ErrorCodeInvalidResponse:
		return "NAgent returned an invalid response"
	case nagent.ErrorCodeResponseTooLarge:
		return "NAgent returned a response that is too large"
	default:
		return "NAgent is unavailable"
	}
}
