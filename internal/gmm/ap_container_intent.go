package gmm

import (
	stdcontext "context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	amf_context "github.com/acore2026/amf/internal/context"
	gmm_message "github.com/acore2026/amf/internal/gmm/message"
	"github.com/acore2026/amf/internal/nagent"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

const (
	apIntentErrorPayloadIDConflict = "PAYLOAD_ID_CONFLICT"
	apIntentErrorQueueFull         = "AMF_QUEUE_FULL"
)

type APIntentJobDispatcher interface {
	Submit(nagent.Job) error
}

type apIntentJobDispatcher = APIntentJobDispatcher

type apIntentResponseSender func(*amf_context.AmfUe, amf_context.APIntentTransaction) bool

type apIntentRuntimeConfig struct {
	Enabled            bool
	Dispatcher         apIntentJobDispatcher
	DispatchUECallback func(uint64, func()) bool
	ResponseTTL        time.Duration
	MaxInFlightPerUE   int
	Sender             apIntentResponseSender
}

var apIntentRuntime = struct {
	sync.RWMutex
	config apIntentRuntimeConfig
}{}

func ConfigureAPIntentIntegration(
	enabled bool,
	dispatcher APIntentJobDispatcher,
	dispatchUECallback func(uint64, func()) bool,
	responseTTL time.Duration,
	maxInFlightPerUE int,
) {
	configureAPIntentRuntime(apIntentRuntimeConfig{
		Enabled:            enabled,
		Dispatcher:         dispatcher,
		DispatchUECallback: dispatchUECallback,
		ResponseTTL:        responseTTL,
		MaxInFlightPerUE:   maxInFlightPerUE,
		Sender:             deliverAPIntentResponse,
	})
}

func configureAPIntentRuntime(config apIntentRuntimeConfig) {
	if config.ResponseTTL <= 0 {
		config.ResponseTTL = time.Minute
	}
	if config.MaxInFlightPerUE <= 0 ||
		config.MaxInFlightPerUE > int(amf_context.MaxAPIntentTransactionsPerUE) {
		config.MaxInFlightPerUE = int(amf_context.MaxAPIntentTransactionsPerUE)
	}
	if config.Sender == nil {
		config.Sender = deliverAPIntentResponse
	}
	apIntentRuntime.Lock()
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
	requestForHTTP := nagent.IntentRequest{
		SUPI:            ue.Supi,
		AccessType:      string(accessType),
		MessageIdentity: messageIdentity,
		ContainerType:   complete.ContainerType,
		PTI:             complete.ContainerTypePTI,
		PayloadID:       complete.ContainerPayloadID,
		Payload:         append([]byte(nil), complete.Payload...),
	}
	request := amf_context.APIntentRequest{
		PayloadID:        complete.ContainerPayloadID,
		RequestHash:      nagent.IntentRequestFingerprint(requestForHTTP),
		MessageIdentity:  messageIdentity,
		AccessType:       accessType,
		ContainerType:    complete.ContainerType,
		ContainerTypePTI: complete.ContainerTypePTI,
		Payload:          append([]byte(nil), complete.Payload...),
	}
	begin, transaction := ue.GetOrCreateCooperationContext().BeginAPIntentWithLimit(
		request, time.Now(), runtime.MaxInFlightPerUE,
	)
	switch begin {
	case amf_context.APIntentBeginPending:
		return groupOrdinaryDLCooperationIEs(ordinary), nil
	case amf_context.APIntentBeginReplay:
		groups, err := buildAPIntentDLGroups(ordinary, transaction)
		if err == nil {
			ue.CooperationContext.MarkAPIntentSent(transaction.PayloadID, transaction.Generation)
		}
		return groups, err
	case amf_context.APIntentBeginConflict:
		return buildImmediateAPIntentError(ordinary, request, apIntentErrorPayloadIDConflict, false, 0)
	case amf_context.APIntentBeginLimit:
		return buildImmediateAPIntentError(ordinary, request, apIntentErrorQueueFull, true, 0)
	case amf_context.APIntentBeginNew:
	default:
		return groupOrdinaryDLCooperationIEs(ordinary), nil
	}

	if runtime.Dispatcher == nil || runtime.DispatchUECallback == nil {
		ue.CooperationContext.RemoveAPIntent(transaction.PayloadID, transaction.Generation)
		return buildImmediateAPIntentError(ordinary, request, nagent.ErrorCodeUnavailable, true, 0)
	}
	jobContext, cancel := stdcontext.WithCancel(stdcontext.Background())
	if !ue.CooperationContext.SetAPIntentCancel(transaction.PayloadID, transaction.Generation, cancel) {
		cancel()
		return groupOrdinaryDLCooperationIEs(ordinary), nil
	}
	ranUe := ue.RanUe[accessType]
	ueID := uint64(0)
	if ranUe != nil {
		ueID = uint64(ranUe.AmfUeNgapId)
	}
	err := runtime.Dispatcher.Submit(nagent.Job{
		Context: jobContext,
		Request: requestForHTTP,
		Callback: func(result nagent.Result) {
			cancel()
			handleAPIntentHTTPResult(runtime, ue, ranUe, ueID, transaction, result)
		},
	})
	if err != nil {
		cancel()
		ue.CooperationContext.RemoveAPIntent(transaction.PayloadID, transaction.Generation)
		code := nagent.ErrorCodeUnavailable
		if errors.Is(err, nagent.ErrQueueFull) {
			code = apIntentErrorQueueFull
		}
		return buildImmediateAPIntentError(ordinary, request, code, true, 0)
	}
	return groupOrdinaryDLCooperationIEs(ordinary), nil
}

func handleAPIntentHTTPResult(
	runtime apIntentRuntimeConfig,
	ue *amf_context.AmfUe,
	expectedRanUe *amf_context.RanUe,
	ueID uint64,
	transaction amf_context.APIntentTransaction,
	result nagent.Result,
) {
	response := append([]byte(nil), result.Response...)
	isError := result.Err != nil
	if isError {
		response = buildAPIntentErrorPayloadFromError(result.Err)
	}
	if !ue.CooperationContext.CompleteAPIntent(
		transaction.PayloadID, transaction.Generation, response, isError, time.Now(), runtime.ResponseTTL,
	) {
		return
	}
	runtime.DispatchUECallback(ueID, func() {
		if expectedRanUe == nil || expectedRanUe.AmfUe != ue {
			return
		}
		deliverReadyAPIntent(ue, transaction.PayloadID, transaction.Generation, runtime.Sender)
	})
}

func deliverReadyAPIntent(
	ue *amf_context.AmfUe,
	payloadID uint16,
	generation uint64,
	sender apIntentResponseSender,
) {
	if ue == nil || ue.CooperationContext == nil {
		return
	}
	transaction, ok := ue.CooperationContext.APIntent(payloadID, generation)
	if !ok || transaction.Status != amf_context.APIntentReady {
		return
	}
	if sender(ue, transaction) {
		ue.CooperationContext.MarkAPIntentSent(payloadID, generation)
	}
}

func sendPendingAPIntentResponses(ue *amf_context.AmfUe, accessType models.AccessType) {
	runtime := currentAPIntentRuntime()
	if !runtime.Enabled || ue == nil || ue.CooperationContext == nil {
		return
	}
	for _, transaction := range ue.CooperationContext.ReadyAPIntents(accessType, time.Now()) {
		deliverReadyAPIntent(ue, transaction.PayloadID, transaction.Generation, runtime.Sender)
	}
}

func deliverAPIntentResponse(ue *amf_context.AmfUe, transaction amf_context.APIntentTransaction) bool {
	ranUe := ue.RanUe[transaction.AccessType]
	if ranUe == nil || !ue.SecurityContextAvailable {
		return false
	}
	container := &nasMessage.APContainer{
		ContainerType:      transaction.ContainerType,
		ContainerTypePTI:   transaction.ContainerTypePTI,
		ContainerPayloadID: transaction.PayloadID,
		ContainerFlags:     0,
		FragmentOffset:     0,
		Payload:            append([]byte(nil), transaction.ResponsePayload...),
	}
	ies, err := buildDLAPContainerIEs(transaction.MessageIdentity, container)
	if err != nil {
		ue.GmmLog.Errorf("Build NAgent DL AP Container failed: %v", err)
		return false
	}
	for _, ie := range ies {
		gmm_message.SendDLCooperation(ranUe, transaction.MessageIdentity, []*nasMessage.CooperationIE{ie})
	}
	return true
}

func buildAPIntentDLGroups(
	ordinary []*nasMessage.CooperationIE,
	transaction amf_context.APIntentTransaction,
) ([][]*nasMessage.CooperationIE, error) {
	container := &nasMessage.APContainer{
		ContainerType:      transaction.ContainerType,
		ContainerTypePTI:   transaction.ContainerTypePTI,
		ContainerPayloadID: transaction.PayloadID,
		Payload:            append([]byte(nil), transaction.ResponsePayload...),
	}
	ies, err := buildDLAPContainerIEs(transaction.MessageIdentity, container)
	if err != nil {
		return nil, err
	}
	return groupDLCooperationResponses(ordinary, ies), nil
}

func buildImmediateAPIntentError(
	ordinary []*nasMessage.CooperationIE,
	request amf_context.APIntentRequest,
	code string,
	retryable bool,
	httpStatus int,
) ([][]*nasMessage.CooperationIE, error) {
	transaction := amf_context.APIntentTransaction{
		APIntentRequest: request,
		ResponsePayload: buildAPIntentErrorPayload(code, retryable, httpStatus),
		ResponseIsError: true,
	}
	return buildAPIntentDLGroups(ordinary, transaction)
}

func buildAPIntentErrorPayloadFromError(err error) []byte {
	code := nagent.ErrorCodeUnavailable
	retryable := true
	httpStatus := 0
	var intentError *nagent.Error
	if errors.As(err, &intentError) {
		code = intentError.Code
		retryable = intentError.Retryable
		httpStatus = intentError.HTTPStatus
	}
	return buildAPIntentErrorPayload(code, retryable, httpStatus)
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
