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
	apIntentResponseContainerType  = 0x0101
	apIntentMaxCallbackReroutes    = 3
	apIntentDispatchRetryDelay     = 20 * time.Millisecond
	apIntentMaxDispatchRetryDelay  = time.Second
)

type APIntentJobDispatcher interface {
	Submit(nagent.Job) error
}

type apIntentJobDispatcher = APIntentJobDispatcher

type apIntentResponseSender func(
	*amf_context.AmfUe,
	*amf_context.RanUe,
	amf_context.APIntentTransaction,
) bool

type apIntentDeliveryResult uint8

const (
	apIntentDeliveryUnavailable apIntentDeliveryResult = iota
	apIntentDeliverySent
	apIntentDeliveryFailed
	apIntentDeliveryAssociationChanged
)

type apIntentRuntimeConfig struct {
	Enabled            bool
	Generation         uint64
	Dispatcher         apIntentJobDispatcher
	DispatchUECallback func(uint64, func()) bool
	RequestTimeout     time.Duration
	ResponseTTL        time.Duration
	MaxInFlightPerUE   int
	Sender             apIntentResponseSender
	AgentRoutes        []nagent.AgentRoute
}

func ConfigureAPIntentIntegration(
	enabled bool,
	dispatcher APIntentJobDispatcher,
	dispatchUECallback func(uint64, func()) bool,
	requestTimeout time.Duration,
	responseTTL time.Duration,
	maxInFlightPerUE int,
	agentRoutes []nagent.AgentRoute,
) {
	configureAPIntentRuntime(apIntentRuntimeConfig{
		Enabled:            enabled,
		Dispatcher:         dispatcher,
		DispatchUECallback: dispatchUECallback,
		RequestTimeout:     requestTimeout,
		ResponseTTL:        responseTTL,
		MaxInFlightPerUE:   maxInFlightPerUE,
		Sender:             deliverAPIntentResponse,
		AgentRoutes:        agentRoutes,
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

func apIntentRuntimeIsActive(runtime apIntentRuntimeConfig) bool {
	current := currentAPIntentRuntime()
	return current.Enabled && current.Generation == runtime.Generation
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
		PendingDLIEs:     cooperationIEData(ordinary),
	}
	request.HTTPRequestID = nagent.IdempotencyKey(requestForHTTP)
	begin, transaction := ue.GetOrCreateCooperationContext().BeginAPIntentWithLimit(
		request, time.Now(), runtime.MaxInFlightPerUE,
	)
	switch begin {
	case amf_context.APIntentBeginPending:
		if transaction.Status == amf_context.APIntentSending {
			return groupOrdinaryDLCooperationIEs(ordinary), nil
		}
		return nil, nil
	case amf_context.APIntentBeginReplay:
		ue.CooperationContext.PrepareAPIntentReplay(transaction.PayloadID, transaction.Generation)
		return nil, nil
	case amf_context.APIntentBeginConflict:
		return buildImmediateAPIntentError(ordinary, request, apIntentErrorPayloadIDConflict, false, 0)
	case amf_context.APIntentBeginLimit:
		return buildImmediateAPIntentError(ordinary, request, apIntentErrorQueueFull, true, 0)
	case amf_context.APIntentBeginNew:
		ue.CooperationContext.StoreCompletedAPContainer(amf_context.CompletedAPContainer{
			ContainerType:      complete.ContainerType,
			ContainerTypePTI:   complete.ContainerTypePTI,
			ContainerPayloadID: complete.ContainerPayloadID,
			Payload:            complete.Payload,
			CompletedAt:        time.Now(),
		})
	default:
		return nil, nil
	}

	if runtime.Dispatcher == nil || runtime.DispatchUECallback == nil {
		ue.CooperationContext.RemoveAPIntent(transaction.PayloadID, transaction.Generation)
		return buildImmediateAPIntentError(ordinary, request, nagent.ErrorCodeUnavailable, true, 0)
	}
	jobContext, cancel := stdcontext.WithTimeout(stdcontext.Background(), runtime.RequestTimeout)
	if !ue.CooperationContext.SetAPIntentCancel(transaction.PayloadID, transaction.Generation, cancel) {
		cancel()
		return nil, nil
	}
	stopDeadline := stdcontext.AfterFunc(jobContext, func() {
		if !errors.Is(jobContext.Err(), stdcontext.DeadlineExceeded) {
			return
		}
		handleAPIntentHTTPResult(runtime, ue, transaction, nagent.Result{
			Request: requestForHTTP,
			Err: &nagent.Error{
				Code:      nagent.ErrorCodeTimeout,
				Retryable: true,
				Cause:     stdcontext.DeadlineExceeded,
			},
		})
	})
	sendAPIntentACK(ue, accessType, messageIdentity, complete)
	if err := runtime.Dispatcher.Submit(nagent.Job{
		Context: jobContext,
		Request: requestForHTTP,
		Callback: func(result nagent.Result) {
			stopDeadline()
			cancel()
			handleAPIntentHTTPResult(runtime, ue, transaction, result)
		},
	}); err != nil {
		stopDeadline()
		cancel()
		ue.CooperationContext.RemoveAPIntent(transaction.PayloadID, transaction.Generation)
		code := nagent.ErrorCodeUnavailable
		if errors.Is(err, nagent.ErrQueueFull) {
			code = apIntentErrorQueueFull
		}
		return buildImmediateAPIntentError(ordinary, request, code, true, 0)
	}
	return nil, nil
}

func sendAPIntentACK(
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	messageIdentity uint8,
	complete *nasMessage.APContainer,
) {
	ackPayload := []byte(`{"$nagent":{"version":1,"status":"accepted"}}`)
	ackContainer := &nasMessage.APContainer{
		ContainerType:      apIntentResponseContainerType,
		ContainerTypePTI:   complete.ContainerTypePTI,
		ContainerPayloadID: complete.ContainerPayloadID,
		ContainerFlags:     nasMessage.APContainerFlagDF,
		FragmentOffset:     0,
		Payload:            ackPayload,
	}
	ackIEs, err := buildDLAPContainerIEs(messageIdentity, ackContainer)
	if err != nil {
		ue.GmmLog.Errorf("Build ACK AP Container failed: %v", err)
		return
	}
	ranUe := ue.APDeliveryRanUe(accessType)
	if ranUe == nil {
		ue.GmmLog.Warn("No RanUe available for ACK delivery")
		return
	}
	if _, err := gmm_message.SendDLCooperationWithResult(ranUe, messageIdentity, ackIEs); err != nil {
		ue.GmmLog.Errorf("Send ACK DLCooperation failed: %v", err)
	}
}

func handleAPIntentHTTPResult(
	runtime apIntentRuntimeConfig,
	ue *amf_context.AmfUe,
	transaction amf_context.APIntentTransaction,
	result nagent.Result,
) {
	if !apIntentRuntimeIsActive(runtime) {
		return
	}
	if transaction.HTTPRequestID != "" &&
		nagent.IdempotencyKey(result.Request) != transaction.HTTPRequestID {
		result.Response = nil
		result.Err = &nagent.Error{
			Code:      nagent.ErrorCodeInvalidResponse,
			Retryable: false,
			Cause:     errors.New("NAgent HTTP result does not match the AP intent transaction"),
		}
	}
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
	currentRanUe := ue.APDeliveryRanUe(transaction.AccessType)
	if currentRanUe == nil {
		return
	}
	dispatchReadyAPIntent(
		runtime, ue, currentRanUe, uint64(currentRanUe.AmfUeNgapId), transaction, 0, 0,
	)
}

func dispatchReadyAPIntent(
	runtime apIntentRuntimeConfig,
	ue *amf_context.AmfUe,
	expectedRanUe *amf_context.RanUe,
	ueID uint64,
	transaction amf_context.APIntentTransaction,
	reroutes int,
	dispatchRetries int,
) {
	callback := func() {
		if !apIntentRuntimeIsActive(runtime) {
			return
		}
		if deliverReadyAPIntent(
			ue, transaction.PayloadID, transaction.Generation, expectedRanUe, true, runtime.Sender,
		) != apIntentDeliveryAssociationChanged || reroutes >= apIntentMaxCallbackReroutes {
			return
		}
		currentRanUe := ue.APDeliveryRanUe(transaction.AccessType)
		if currentRanUe == nil {
			return
		}
		dispatchReadyAPIntent(
			runtime,
			ue,
			currentRanUe,
			uint64(currentRanUe.AmfUeNgapId),
			transaction,
			reroutes+1,
			0,
		)
	}
	if runtime.DispatchUECallback != nil && runtime.DispatchUECallback(ueID, callback) {
		return
	}
	if ue == nil || ue.CooperationContext == nil {
		return
	}
	candidate, ok := ue.CooperationContext.APIntent(transaction.PayloadID, transaction.Generation)
	if !ok || candidate.Status != amf_context.APIntentReady {
		return
	}
	shift := dispatchRetries
	if shift > 6 {
		shift = 6
	}
	delay := apIntentDispatchRetryDelay * time.Duration(1<<shift)
	if delay > apIntentMaxDispatchRetryDelay {
		delay = apIntentMaxDispatchRetryDelay
	}
	time.AfterFunc(delay, func() {
		if !apIntentRuntimeIsActive(runtime) {
			return
		}
		dispatchReadyAPIntent(
			runtime, ue, expectedRanUe, ueID, transaction, reroutes, dispatchRetries+1,
		)
	})
}

func deliverReadyAPIntent(
	ue *amf_context.AmfUe,
	payloadID uint16,
	generation uint64,
	expectedRanUe *amf_context.RanUe,
	requireRegistered bool,
	sender apIntentResponseSender,
) (result apIntentDeliveryResult) {
	if ue == nil || ue.CooperationContext == nil {
		return apIntentDeliveryUnavailable
	}
	candidate, ok := ue.CooperationContext.APIntent(payloadID, generation)
	if !ok || candidate.Status != amf_context.APIntentReady {
		return apIntentDeliveryUnavailable
	}
	accessType := candidate.AccessType
	ranUe, deliveryRanUe, associationGeneration, associated :=
		ue.APDeliveryRanUeSnapshot(accessType, expectedRanUe)
	if !associated {
		if ue.APDeliveryRanUe(accessType) != nil {
			return apIntentDeliveryAssociationChanged
		}
		return apIntentDeliveryUnavailable
	}
	if requireRegistered {
		state := ue.State[accessType]
		if state == nil || !state.Is(amf_context.Registered) {
			return apIntentDeliveryUnavailable
		}
	}
	transaction, claimed := ue.CooperationContext.ClaimAPIntentDelivery(payloadID, generation)
	if !claimed || transaction.AccessType != accessType {
		return apIntentDeliveryUnavailable
	}
	finished := false
	defer func() {
		if !finished {
			_, _ = ue.CooperationContext.FinishAPIntentDeliveryAttempt(
				payloadID, generation, transaction.DeliveryAttempt, false,
			)
		}
	}()
	if !ue.APDeliveryRanUeSnapshotCurrent(accessType, ranUe, associationGeneration) {
		_, finished = ue.CooperationContext.FinishAPIntentDeliveryAttempt(
			payloadID, generation, transaction.DeliveryAttempt, false,
		)
		return apIntentDeliveryAssociationChanged
	}
	sent := sender(ue, deliveryRanUe, transaction)
	associationCurrent := ue.APDeliveryRanUeSnapshotCurrent(accessType, ranUe, associationGeneration)
	commitSent := sent && associationCurrent
	status, didFinish := ue.CooperationContext.FinishAPIntentDeliveryAttempt(
		payloadID, generation, transaction.DeliveryAttempt, commitSent,
	)
	finished = didFinish
	if !finished {
		return apIntentDeliveryUnavailable
	}
	if status == amf_context.APIntentSent {
		return apIntentDeliverySent
	}
	if !associationCurrent {
		return apIntentDeliveryAssociationChanged
	}
	return apIntentDeliveryFailed
}

func deliverReadyAPIntentFollowingAssociation(
	ue *amf_context.AmfUe,
	payloadID uint16,
	generation uint64,
	requireRegistered bool,
	sender apIntentResponseSender,
) apIntentDeliveryResult {
	for attempts := 0; attempts <= apIntentMaxCallbackReroutes; attempts++ {
		result := deliverReadyAPIntent(ue, payloadID, generation, nil, requireRegistered, sender)
		if result != apIntentDeliveryAssociationChanged {
			return result
		}
	}
	return apIntentDeliveryAssociationChanged
}

func sendPendingAPIntentResponses(
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	requireRegistered bool,
) {
	runtime := currentAPIntentRuntime()
	if !runtime.Enabled || ue == nil || ue.CooperationContext == nil {
		return
	}
	for _, transaction := range ue.CooperationContext.ReadyAPIntents(accessType, time.Now()) {
		deliverReadyAPIntentFollowingAssociation(
			ue, transaction.PayloadID, transaction.Generation, requireRegistered, runtime.Sender,
		)
	}
}

func NotifyAPIntentDeliveryAvailable(ue *amf_context.AmfUe, accessType models.AccessType) {
	sendPendingAPIntentResponses(ue, accessType, true)
}

// HandleAPIntentNASNonDelivery returns a matched transport-submitted AP intent
// response to Ready. It is retried when the UE next becomes delivery-available.
func HandleAPIntentNASNonDelivery(
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	nasPDU []byte,
) bool {
	if ue == nil || ue.CooperationContext == nil {
		return false
	}
	transaction, matched := ue.CooperationContext.MarkAPIntentNASNonDelivery(accessType, nasPDU)
	if !matched {
		return false
	}
	ue.GmmLog.Warnf(
		"NAgent DL AP Container was not delivered payloadId=0x%04x attempt=%d; waiting for next delivery opportunity",
		transaction.PayloadID, transaction.DeliveryAttempt,
	)
	return true
}

func deliverAPIntentResponse(
	ue *amf_context.AmfUe,
	ranUe *amf_context.RanUe,
	transaction amf_context.APIntentTransaction,
) bool {
	if ranUe == nil || !ue.SecurityContextAvailable {
		return false
	}
	ordinary, err := cooperationIEsFromData(transaction.PendingDLIEs)
	if err != nil {
		ue.GmmLog.Errorf("Restore pending DL Cooperation IEs failed: %v", err)
		return false
	}
	groups, err := buildAPIntentDLGroups(ordinary, transaction)
	if err != nil {
		ue.GmmLog.Errorf("Build NAgent DL AP Container failed: %v", err)
		return false
	}
	for index, ies := range groups {
		if !ue.APDeliveryTargetCurrent(transaction.AccessType, ranUe) {
			return false
		}
		nasPDU, err := gmm_message.SendDLCooperationWithResult(
			ranUe, transaction.MessageIdentity, ies,
		)
		if err != nil {
			ue.GmmLog.Errorf(
				"Send NAgent DL AP Container failed payloadId=0x%04x message=%d/%d: %v",
				transaction.PayloadID, index+1, len(groups), err,
			)
			return false
		}
		if !ue.CooperationContext.RecordAPIntentDLNAS(
			transaction.PayloadID,
			transaction.Generation,
			transaction.DeliveryAttempt,
			nasPDU,
		) {
			ue.GmmLog.Errorf(
				"Track NAgent DL AP Container failed payloadId=0x%04x attempt=%d message=%d/%d",
				transaction.PayloadID, transaction.DeliveryAttempt, index+1, len(groups),
			)
			return false
		}
		if !ue.APDeliveryTargetCurrent(transaction.AccessType, ranUe) {
			return false
		}
	}
	return true
}

func buildAPIntentDLGroups(
	ordinary []*nasMessage.CooperationIE,
	transaction amf_context.APIntentTransaction,
) ([][]*nasMessage.CooperationIE, error) {
	container := &nasMessage.APContainer{
		ContainerType:      apIntentResponseContainerType,
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
	code, retryable, httpStatus := apIntentErrorDetails(err)
	return buildAPIntentErrorPayload(code, retryable, httpStatus)
}

func apIntentErrorDetails(err error) (code string, retryable bool, httpStatus int) {
	code = nagent.ErrorCodeUnavailable
	retryable = true
	var intentError *nagent.Error
	if errors.As(err, &intentError) {
		code = intentError.Code
		retryable = intentError.Retryable
		httpStatus = intentError.HTTPStatus
	}
	return code, retryable, httpStatus
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
