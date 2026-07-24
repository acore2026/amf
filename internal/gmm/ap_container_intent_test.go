package gmm

import (
	"bytes"
	stdcontext "context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	amf_context "github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/logger"
	"github.com/acore2026/amf/internal/nagent"
	ngaptesting "github.com/acore2026/amf/internal/ngap/testing"
	"github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/ngap"
	"github.com/acore2026/ngap/ngapType"
	"github.com/acore2026/openapi/models"
	"github.com/acore2026/util/fsm"
)

type fakeAPIntentDispatcher struct {
	mu   sync.Mutex
	jobs []nagent.Job
	err  error
}

func (f *fakeAPIntentDispatcher) Submit(job nagent.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, job)
	return nil
}

func (f *fakeAPIntentDispatcher) Jobs() []nagent.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]nagent.Job(nil), f.jobs...)
}

func TestAPIntentWaitsToDeliverOrdinaryIEWithEcho(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan amf_context.APIntentTransaction, 1)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		transaction amf_context.APIntentTransaction,
	) bool {
		delivered <- transaction
		return true
	})
	ue := testAPIntentUE(t)
	payload := []byte(`{"intent":"locate","target":"cell-1"}`)
	ul := testAPIntentUL(t, 0x1234, payload)
	mustAddIE(t, ul, 0x10, []byte{0x01})

	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}
	if len(dlMessages) != 0 {
		t.Fatalf("DL messages before HTTP response = %#v, want none", dlMessages)
	}
	jobs := dispatcher.Jobs()
	wantPayload := testAdaptedIntentPayload(t, testIntentPayload(t, payload), ue.Supi)
	if len(jobs) != 1 || !bytes.Equal(jobs[0].Request.Payload, wantPayload) {
		t.Fatalf("jobs = %#v", jobs)
	}

	jobs[0].Callback(nagent.Result{Request: jobs[0].Request, Response: payload})
	select {
	case transaction := <-delivered:
		if transaction.PayloadID != 0x1234 || transaction.ContainerType != apIntentResponseContainerType ||
			transaction.ContainerTypePTI != 5 || transaction.MessageIdentity != 1 ||
			string(transaction.ResponsePayload) != string(payload) || transaction.ResponseIsError {
			t.Fatalf("delivered transaction = %#v", transaction)
		}
		if len(transaction.PendingDLIEs) != 1 || transaction.PendingDLIEs[0].IEI != 0x10 ||
			!bytes.Equal(transaction.PendingDLIEs[0].Contents, []byte{0x01}) {
			t.Fatalf("pending DL IEs = %#v, want IE 0x10", transaction.PendingDLIEs)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP result was not delivered")
	}
}

func TestBuildAPIntentDLGroupsCombinesPendingOrdinaryIEOnlyWithFirstFragment(t *testing.T) {
	transaction := amf_context.APIntentTransaction{
		APIntentRequest: amf_context.APIntentRequest{
			PayloadID:        0x1234,
			MessageIdentity:  1,
			ContainerType:    apIntentResponseContainerType,
			ContainerTypePTI: 5,
			PendingDLIEs: []amf_context.DLCooperationIE{
				{IEI: 0x10, Contents: []byte{0x01}},
			},
		},
		ResponsePayload: bytes.Repeat([]byte{0xaa}, 500),
	}
	ordinary, err := cooperationIEsFromData(transaction.PendingDLIEs)
	if err != nil {
		t.Fatalf("cooperationIEsFromData() error = %v", err)
	}
	groups, err := buildAPIntentDLGroups(ordinary, transaction)
	if err != nil {
		t.Fatalf("buildAPIntentDLGroups() error = %v", err)
	}
	if len(groups) != 3 || len(groups[0]) != 2 || len(groups[1]) != 1 || len(groups[2]) != 1 {
		t.Fatalf("DL groups = %#v", groups)
	}
	assertDLIE(t, groups[0][0], 0x10, []byte{0x01})
	assertAPContainerIE(t, groups[0][1], 0, true, transaction.ResponsePayload[:245])
	firstAP, err := nasMessage.DecodeAPContainer(groups[0][1].GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if firstAP.ContainerType != apIntentResponseContainerType || firstAP.ContainerTypePTI != 5 ||
		firstAP.ContainerPayloadID != 0x1234 {
		t.Fatalf("DL AP correlation fields = %#v", firstAP)
	}
	assertAPContainerIE(t, groups[1][0], 245, true, transaction.ResponsePayload[245:490])
	assertAPContainerIE(t, groups[2][0], 490, false, transaction.ResponsePayload[490:])
}

func TestAPIntentRuntimeDefaultsToThreeSecondRequestDeadline(t *testing.T) {
	configureAPIntentRuntime(apIntentRuntimeConfig{})
	t.Cleanup(func() { configureAPIntentRuntime(apIntentRuntimeConfig{}) })
	if got := currentAPIntentRuntime().RequestTimeout; got != 3*time.Second {
		t.Fatalf("default request timeout = %v, want 3s", got)
	}
}

func TestAPIntentRejectsInvalidULIntentWithoutSubmittingHTTP(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		code    string
	}{
		{name: "invalid JSON", payload: []byte(`{"intentId":`), code: nagent.ErrorCodeInvalidJSON},
		{name: "missing fields", payload: []byte(`{"intentId":"one"}`), code: nagent.ErrorCodeInvalidRequest},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := &fakeAPIntentDispatcher{}
			configureTestAPIntent(t, dispatcher, func(
				*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction,
			) bool {
				return true
			})
			ue := testAPIntentUE(t)
			ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
			ul.MessageIdentity = 1
			mustAddAPContainerIE(t, ul, &nasMessage.APContainer{
				ContainerType:      apIntentResponseContainerType,
				ContainerTypePTI:   5,
				ContainerPayloadID: uint16(40 + index),
				ContainerFlags:     nasMessage.APContainerFlagDF,
				Payload:            test.payload,
			})

			dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
			if err != nil {
				t.Fatalf("processULCooperationIEs() error = %v", err)
			}
			assertAPIntentErrorCode(t, dlMessages, test.code)
			if len(dispatcher.Jobs()) != 0 {
				t.Fatal("invalid Intent was submitted to NAgent")
			}
			ap, err := nasMessage.DecodeAPContainer(dlMessages[0][0].GetContents())
			if err != nil {
				t.Fatalf("DecodeAPContainer() error = %v", err)
			}
			if ap.ContainerType != apIntentResponseContainerType || ap.ContainerTypePTI != 5 {
				t.Fatalf("invalid Intent DL AP fields = %#v", ap)
			}
		})
	}
}

func TestAPIntentHTTPResultMustMatchStoredRequestIDAndPTI(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan amf_context.APIntentTransaction, 1)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		transaction amf_context.APIntentTransaction,
	) bool {
		delivered <- transaction
		return true
	})
	ue := testAPIntentUE(t)
	if dlMessages, err := processULCooperationIEs(
		ue,
		models.AccessType__3_GPP_ACCESS,
		testAPIntentUL(t, 44, []byte(`{"intent":"correlate"}`)),
	); err != nil || len(dlMessages) != 0 {
		t.Fatalf("process messages=%#v error=%v", dlMessages, err)
	}
	job := dispatcher.Jobs()[0]
	stored, ok := ue.CooperationContext.APIntent(44, 1)
	if !ok || stored.HTTPRequestID != nagent.IdempotencyKey(job.Request) || stored.ContainerTypePTI != 5 {
		t.Fatalf("stored HTTP/PTI mapping = %#v, ok=%v", stored, ok)
	}

	mismatched := job.Request
	mismatched.PTI++
	job.Callback(nagent.Result{Request: mismatched, Response: []byte(`{"result":"wrong-request"}`)})
	transaction := <-delivered
	if !transaction.ResponseIsError || transaction.ContainerTypePTI != 5 {
		t.Fatalf("mismatched HTTP result transaction = %#v", transaction)
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(transaction.ResponsePayload, &payload); err != nil {
		t.Fatalf("error payload is invalid JSON: %v", err)
	}
	if payload["$nagent"]["code"] != nagent.ErrorCodeInvalidResponse {
		t.Fatalf("mismatched HTTP result payload = %s", transaction.ResponsePayload)
	}
}

func TestAPIntentPendingDuplicateDoesNotSubmitAgainAndConflictReturnsError(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	configureTestAPIntent(t, dispatcher, func(
		*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction,
	) bool {
		return true
	})
	ue := testAPIntentUE(t)

	first := testAPIntentUL(t, 7, []byte(`{"intent":"first"}`))
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, first); err != nil {
		t.Fatalf("first process error = %v", err)
	}
	duplicate := testAPIntentUL(t, 7, []byte(`{"intent":"first"}`))
	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, duplicate)
	if err != nil || len(dlMessages) != 0 || len(dispatcher.Jobs()) != 1 {
		t.Fatalf("duplicate result messages=%#v jobs=%d err=%v", dlMessages, len(dispatcher.Jobs()), err)
	}

	conflict := testAPIntentUL(t, 7, []byte(`{"intent":"different"}`))
	dlMessages, err = processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, conflict)
	if err != nil {
		t.Fatalf("conflict process error = %v", err)
	}
	assertAPIntentErrorCode(t, dlMessages, "PAYLOAD_ID_CONFLICT")
	if len(dispatcher.Jobs()) != 1 {
		t.Fatalf("conflict submitted another HTTP job: %d", len(dispatcher.Jobs()))
	}
	completed := ue.CooperationContext.CompletedAPContainers()[7]
	if !bytes.Equal(completed.Payload, testIntentPayload(t, []byte(`{"intent":"first"}`))) {
		t.Fatalf("conflict replaced completed payload: %s", completed.Payload)
	}

	metadataConflict := testAPIntentUL(t, 7, []byte(`{"intent":"first"}`))
	metadataAP, err := nasMessage.DecodeAPContainer(metadataConflict.GetIE(0x71).GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	metadataAP.ContainerTypePTI++
	encodedMetadataAP, err := metadataAP.Encode()
	if err != nil {
		t.Fatalf("APContainer.Encode() error = %v", err)
	}
	metadataConflict.GetIE(0x71).SetContents(encodedMetadataAP)
	dlMessages, err = processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, metadataConflict)
	if err != nil {
		t.Fatalf("metadata conflict process error = %v", err)
	}
	assertAPIntentErrorCode(t, dlMessages, "PAYLOAD_ID_CONFLICT")
}

func TestAPIntentDispatchRetriesAfterQueueRejection(t *testing.T) {
	ue := testAPIntentUE(t)
	transaction := readyAPIntentTransaction(t, ue, 19)
	delivered := make(chan struct{}, 1)
	attempts := 0
	configureAPIntentRuntime(apIntentRuntimeConfig{
		Enabled: true,
		DispatchUECallback: func(_ uint64, callback func()) bool {
			attempts++
			if attempts == 1 {
				return false
			}
			callback()
			return true
		},
		ResponseTTL: time.Minute,
		Sender: func(*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction) bool {
			delivered <- struct{}{}
			return true
		},
	})
	t.Cleanup(func() { configureAPIntentRuntime(apIntentRuntimeConfig{}) })
	runtime := currentAPIntentRuntime()

	dispatchReadyAPIntent(
		runtime, ue, ue.APDeliveryRanUe(models.AccessType__3_GPP_ACCESS), 42, transaction, 0, 0,
	)
	select {
	case <-delivered:
		if attempts != 2 {
			t.Fatalf("dispatch attempts = %d, want 2", attempts)
		}
	case <-time.After(time.Second):
		t.Fatal("AP response was not dispatched after a transient queue rejection")
	}
}

func TestAPIntentReadyReplayMergesOrdinaryIEBeforeDrain(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	deliveries := 0
	configureTestAPIntent(t, dispatcher, func(
		*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction,
	) bool {
		deliveries++
		return false
	})
	ue := testAPIntentUE(t)
	first := testAPIntentUL(t, 21, []byte(`{"intent":"replay"}`))
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, first); err != nil {
		t.Fatalf("first process error = %v", err)
	}
	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"result":"ready"}`)})
	if deliveries != 1 {
		t.Fatalf("initial deliveries = %d, want 1 failed attempt", deliveries)
	}

	duplicate := testAPIntentUL(t, 21, []byte(`{"intent":"replay"}`))
	mustAddIE(t, duplicate, 0x10, []byte{0x01})
	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, duplicate)
	if err != nil {
		t.Fatalf("duplicate process error = %v", err)
	}
	if deliveries != 1 || len(dlMessages) != 0 {
		t.Fatalf("pre-drain deliveries=%d messages=%#v, want no early DL", deliveries, dlMessages)
	}

	sendPendingAPIntentResponses(ue, models.AccessType__3_GPP_ACCESS, true)
	if deliveries != 2 {
		t.Fatalf("deliveries after drain = %d, want 2", deliveries)
	}
}

func TestAPIntentSentReplayReturnsToReadyWithoutEarlyOrdinaryDL(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	deliveries := 0
	configureTestAPIntent(t, dispatcher, func(
		*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction,
	) bool {
		deliveries++
		return true
	})
	ue := testAPIntentUE(t)
	first := testAPIntentUL(t, 22, []byte(`{"intent":"sent-replay"}`))
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, first); err != nil {
		t.Fatalf("first process error = %v", err)
	}
	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"result":"sent"}`)})
	if deliveries != 1 {
		t.Fatalf("initial deliveries = %d, want 1", deliveries)
	}

	duplicate := testAPIntentUL(t, 22, []byte(`{"intent":"sent-replay"}`))
	mustAddIE(t, duplicate, 0x10, []byte{0x01})
	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, duplicate)
	if err != nil {
		t.Fatalf("duplicate process error = %v", err)
	}
	if deliveries != 1 || len(dlMessages) != 0 {
		t.Fatalf("pre-drain deliveries=%d messages=%#v", deliveries, dlMessages)
	}
	current, ok := ue.CooperationContext.APIntent(22, 1)
	if !ok || current.Status != amf_context.APIntentReady {
		t.Fatalf("replay transaction = %#v, ok=%v", current, ok)
	}

	sendPendingAPIntentResponses(ue, models.AccessType__3_GPP_ACCESS, true)
	if deliveries != 2 {
		t.Fatalf("deliveries after drain = %d, want 2", deliveries)
	}
}

func TestAPIntentDuplicateDuringDLSendReturnsOrdinaryOnlyAfterHTTPCompletion(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSend) }) }
	t.Cleanup(release)
	configureTestAPIntent(t, dispatcher, func(
		*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction,
	) bool {
		close(sendStarted)
		<-releaseSend
		return true
	})
	ue := testAPIntentUE(t)
	first := testAPIntentUL(t, 32, []byte(`{"intent":"sending"}`))
	if dlMessages, err := processULCooperationIEs(
		ue, models.AccessType__3_GPP_ACCESS, first,
	); err != nil || len(dlMessages) != 0 {
		t.Fatalf("initial process messages=%#v error=%v", dlMessages, err)
	}

	job := dispatcher.Jobs()[0]
	callbackDone := make(chan struct{})
	go func() {
		job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"result":"ok"}`)})
		close(callbackDone)
	}()
	select {
	case <-sendStarted:
	case <-time.After(time.Second):
		t.Fatal("DL AP response did not start after HTTP completion")
	}

	duplicate := testAPIntentUL(t, 32, []byte(`{"intent":"sending"}`))
	mustAddIE(t, duplicate, 0x10, []byte{0x01})
	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, duplicate)
	if err != nil {
		t.Fatalf("duplicate process error = %v", err)
	}
	if len(dlMessages) != 1 || len(dlMessages[0]) != 1 {
		t.Fatalf("duplicate DL messages = %#v, want ordinary response after HTTP completion", dlMessages)
	}
	assertDLIE(t, dlMessages[0][0], 0x10, []byte{0x01})

	release()
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("HTTP callback did not finish")
	}
}

func TestAPIntentHTTPErrorBecomesCanonicalDLJSON(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan amf_context.APIntentTransaction, 1)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		transaction amf_context.APIntentTransaction,
	) bool {
		delivered <- transaction
		return true
	})
	ue := testAPIntentUE(t)
	ul := testAPIntentUL(t, 9, []byte(`{"intent":"timeout"}`))
	_, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("process error = %v", err)
	}
	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Err: &nagent.Error{
		Code: nagent.ErrorCodeTimeout, Retryable: true,
	}})

	transaction := <-delivered
	if !transaction.ResponseIsError {
		t.Fatal("HTTP failure was not marked as error")
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(transaction.ResponsePayload, &payload); err != nil {
		t.Fatalf("error payload is invalid JSON: %v", err)
	}
	if payload["$nagent"]["code"] != nagent.ErrorCodeTimeout || payload["$nagent"]["retryable"] != true {
		t.Fatalf("error payload = %s", transaction.ResponsePayload)
	}
}

func TestAPIntentReadyResponseWaitsForUEReconnect(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	var deliveries int
	configureTestAPIntent(t, dispatcher, func(
		ue *amf_context.AmfUe,
		_ *amf_context.RanUe,
		_ amf_context.APIntentTransaction,
	) bool {
		if ue.RanUeForAccessType(models.AccessType__3_GPP_ACCESS) == nil {
			return false
		}
		deliveries++
		return true
	})
	dispatches := 0
	runtime := currentAPIntentRuntime()
	runtime.DispatchUECallback = func(_ uint64, callback func()) bool {
		dispatches++
		callback()
		return true
	}
	configureAPIntentRuntime(runtime)
	ue := testAPIntentUE(t)
	ul := testAPIntentUL(t, 11, []byte(`{"intent":"offline"}`))
	_, _ = processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	ue.DetachRanUe(models.AccessType__3_GPP_ACCESS)
	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"result":"ready"}`)})
	if deliveries != 0 || dispatches != 0 {
		t.Fatalf("offline response deliveries=%d callback dispatches=%d", deliveries, dispatches)
	}

	ue.AttachRanUe(&amf_context.RanUe{
		AmfUeNgapId: 42,
		Ran:         &amf_context.AmfRan{AnType: models.AccessType__3_GPP_ACCESS},
	})
	sendPendingAPIntentResponses(ue, models.AccessType__3_GPP_ACCESS, true)
	if deliveries != 1 {
		t.Fatalf("deliveries = %d, want 1", deliveries)
	}
}

func TestAPIntentDeadlineStartsWhenJobIsQueuedAndIgnoresLateResult(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan amf_context.APIntentTransaction, 2)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		transaction amf_context.APIntentTransaction,
	) bool {
		delivered <- transaction
		return true
	})
	runtime := currentAPIntentRuntime()
	runtime.RequestTimeout = 30 * time.Millisecond
	configureAPIntentRuntime(runtime)

	ue := testAPIntentUE(t)
	ul := testAPIntentUL(t, 12, []byte(`{"intent":"queued"}`))
	mustAddIE(t, ul, 0x10, []byte{0x01})
	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("process error = %v", err)
	}
	if len(dlMessages) != 0 {
		t.Fatalf("DL messages before timeout = %#v, want none", dlMessages)
	}
	job := dispatcher.Jobs()[0]

	select {
	case transaction := <-delivered:
		var payload map[string]map[string]any
		if err := json.Unmarshal(transaction.ResponsePayload, &payload); err != nil {
			t.Fatalf("timeout payload is invalid JSON: %v", err)
		}
		if payload["$nagent"]["code"] != nagent.ErrorCodeTimeout {
			t.Fatalf("timeout payload = %s", transaction.ResponsePayload)
		}
		if len(transaction.PendingDLIEs) != 1 || transaction.PendingDLIEs[0].IEI != 0x10 ||
			!bytes.Equal(transaction.PendingDLIEs[0].Contents, []byte{0x01}) {
			t.Fatalf("timeout pending DL IEs = %#v", transaction.PendingDLIEs)
		}
	case <-time.After(time.Second):
		t.Fatal("queued AP intent did not time out")
	}

	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"late":true}`)})
	select {
	case duplicate := <-delivered:
		t.Fatalf("late HTTP result was delivered: %#v", duplicate)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServiceRequestRejectDoesNotDrainReadyAPIntent(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	deliveries := 0
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		_ amf_context.APIntentTransaction,
	) bool {
		deliveries++
		return true
	})
	ue := testAPIntentUE(t)
	ue.State[models.AccessType__3_GPP_ACCESS].Set(amf_context.Deregistered)
	ul := testAPIntentUL(t, 13, []byte(`{"intent":"wait-for-service"}`))
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul); err != nil {
		t.Fatalf("process error = %v", err)
	}
	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"ready":true}`)})

	handleServiceRequestOutcome(ue, models.AccessType__3_GPP_ACCESS, ServiceRequestRejected, nil)
	if deliveries != 0 {
		t.Fatalf("deliveries after reject = %d, want 0", deliveries)
	}
	handleServiceRequestOutcome(ue, models.AccessType__3_GPP_ACCESS, ServiceRequestAccepted, nil)
	if deliveries != 1 {
		t.Fatalf("deliveries after accept = %d, want 1", deliveries)
	}
}

func TestAPIntentDisabledRuntimeIgnoresInFlightCompletionAndDeadline(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan struct{}, 1)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		_ amf_context.APIntentTransaction,
	) bool {
		delivered <- struct{}{}
		return true
	})
	runtime := currentAPIntentRuntime()
	runtime.RequestTimeout = 30 * time.Millisecond
	configureAPIntentRuntime(runtime)

	ue := testAPIntentUE(t)
	ul := testAPIntentUL(t, 15, []byte(`{"intent":"shutdown"}`))
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul); err != nil {
		t.Fatalf("process error = %v", err)
	}
	job := dispatcher.Jobs()[0]
	configureAPIntentRuntime(apIntentRuntimeConfig{})
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"late":true}`)})

	select {
	case <-delivered:
		t.Fatal("disabled runtime delivered an in-flight AP response")
	case <-time.After(80 * time.Millisecond):
	}
	transaction, ok := ue.CooperationContext.APIntent(15, 1)
	if !ok || transaction.Status != amf_context.APIntentPending {
		t.Fatalf("transaction after shutdown = %#v, ok=%v", transaction, ok)
	}
}

func TestAPIntentStaleRanUeCallbackReroutesToHandoverTarget(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	deliveredRanUe := make(chan *amf_context.RanUe, 2)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		ranUe *amf_context.RanUe,
		_ amf_context.APIntentTransaction,
	) bool {
		deliveredRanUe <- ranUe
		return true
	})
	ue := testAPIntentUE(t)
	source := ue.RanUeForAccessType(models.AccessType__3_GPP_ACCESS)
	source.Ran = &amf_context.AmfRan{AnType: models.AccessType__3_GPP_ACCESS}
	ul := testAPIntentUL(t, 14, []byte(`{"intent":"handover"}`))
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul); err != nil {
		t.Fatalf("process error = %v", err)
	}
	target := &amf_context.RanUe{
		AmfUeNgapId: 43,
		Ran:         &amf_context.AmfRan{AnType: models.AccessType__3_GPP_ACCESS},
	}
	ue.AttachRanUe(target)
	source.DetachAmfUe()

	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"target":true}`)})
	select {
	case ranUe := <-deliveredRanUe:
		if ranUe == nil || ranUe == target ||
			ranUe.AmfUeNgapId != target.AmfUeNgapId || ranUe.Ran != target.Ran {
			t.Fatalf("delivered RanUe = %#v, want target", ranUe)
		}
	case <-time.After(time.Second):
		t.Fatal("stale callback was not rerouted to the handover target")
	}

	NotifyAPIntentDeliveryAvailable(ue, models.AccessType__3_GPP_ACCESS)
	select {
	case ranUe := <-deliveredRanUe:
		t.Fatalf("handover wakeup delivered a duplicate on RanUe %#v", ranUe)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestAPIntentFailedSendStaysReadyForRetry(t *testing.T) {
	ue := testAPIntentUE(t)
	transaction := readyAPIntentTransaction(t, ue, 16)
	attempts := 0
	sender := func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		_ amf_context.APIntentTransaction,
	) bool {
		attempts++
		return attempts > 1
	}

	result := deliverReadyAPIntent(
		ue, transaction.PayloadID, transaction.Generation, nil, true, sender,
	)
	if result != apIntentDeliveryFailed {
		t.Fatalf("first delivery result=%v, want failed", result)
	}
	current, ok := ue.CooperationContext.APIntent(transaction.PayloadID, transaction.Generation)
	if !ok || current.Status != amf_context.APIntentReady {
		t.Fatalf("transaction after failed send = %#v, ok=%v", current, ok)
	}

	result = deliverReadyAPIntent(
		ue, transaction.PayloadID, transaction.Generation, nil, true, sender,
	)
	if result != apIntentDeliverySent {
		t.Fatalf("retry delivery result=%v, want sent", result)
	}
	current, ok = ue.CooperationContext.APIntent(transaction.PayloadID, transaction.Generation)
	if !ok || current.Status != amf_context.APIntentSent {
		t.Fatalf("transaction after retry = %#v, ok=%v", current, ok)
	}
}

func TestAPIntentNASNonDeliveryReturnsTransportSentResponseToReady(t *testing.T) {
	ue := testAPIntentUE(t)
	ue.SecurityContextAvailable = true
	ue.CipheringAlg = 0
	ue.IntegrityAlg = 0
	conn := &ngaptesting.SctpConnStub{}
	ran := &amf_context.AmfRan{
		AnType: models.AccessType__3_GPP_ACCESS,
		Conn:   conn,
		Log:    logger.NgapLog,
	}
	ue.AttachRanUe(&amf_context.RanUe{
		RanUeNgapId: 11,
		AmfUeNgapId: 42,
		Ran:         ran,
		Log:         logger.NgapLog,
	})
	transaction := readyAPIntentTransaction(t, ue, 26)

	result := deliverReadyAPIntent(
		ue,
		transaction.PayloadID,
		transaction.Generation,
		nil,
		true,
		deliverAPIntentResponse,
	)
	if result != apIntentDeliverySent {
		t.Fatalf("delivery result=%v, want transport sent", result)
	}
	if len(conn.MsgList) != 1 {
		t.Fatalf("NGAP writes=%d, want 1", len(conn.MsgList))
	}
	message, err := ngap.Decoder(conn.MsgList[0])
	if err != nil {
		t.Fatalf("decode Downlink NAS Transport: %v", err)
	}
	var nasPDU []byte
	for _, ie := range message.InitiatingMessage.Value.DownlinkNASTransport.ProtocolIEs.List {
		if ie.Value.Present == ngapType.DownlinkNASTransportIEsPresentNASPDU {
			nasPDU = append([]byte(nil), ie.Value.NASPDU.Value...)
			break
		}
	}
	if len(nasPDU) == 0 {
		t.Fatal("Downlink NAS Transport did not contain a NAS PDU")
	}
	if HandleAPIntentNASNonDelivery(ue, models.AccessType_NON_3_GPP_ACCESS, nasPDU) {
		t.Fatal("NAS Non-Delivery matched the wrong access type")
	}
	if !HandleAPIntentNASNonDelivery(ue, models.AccessType__3_GPP_ACCESS, nasPDU) {
		t.Fatal("NAS Non-Delivery did not match the submitted AP response")
	}
	current, ok := ue.CooperationContext.APIntent(transaction.PayloadID, transaction.Generation)
	if !ok || current.Status != amf_context.APIntentReady {
		t.Fatalf("transaction after NAS Non-Delivery = %#v, ok=%v", current, ok)
	}
	if HandleAPIntentNASNonDelivery(ue, models.AccessType__3_GPP_ACCESS, nasPDU) {
		t.Fatal("duplicate NAS Non-Delivery matched twice")
	}
}

func TestDeliverAPIntentResponseReportsNGAPPreconditionFailure(t *testing.T) {
	ue := testAPIntentUE(t)
	ue.SecurityContextAvailable = true
	ranUe := &amf_context.RanUe{AmfUe: ue}
	transaction := amf_context.APIntentTransaction{
		APIntentRequest: amf_context.APIntentRequest{
			PayloadID:        18,
			MessageIdentity:  1,
			AccessType:       models.AccessType__3_GPP_ACCESS,
			ContainerType:    apIntentResponseContainerType,
			ContainerTypePTI: 5,
		},
		ResponsePayload: []byte(`{"result":"ok"}`),
	}
	if deliverAPIntentResponse(ue, ranUe, transaction) {
		t.Fatal("delivery succeeded without a Ran connection")
	}
}

func TestAPIntentBlockedSendDoesNotBlockRanUeSwitch(t *testing.T) {
	ue := testAPIntentUE(t)
	source := ue.APDeliveryRanUe(models.AccessType__3_GPP_ACCESS)
	transaction := readyAPIntentTransaction(t, ue, 17)
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	resultCh := make(chan apIntentDeliveryResult, 1)
	go func() {
		resultCh <- deliverReadyAPIntent(
			ue,
			transaction.PayloadID,
			transaction.Generation,
			source,
			true,
			func(*amf_context.AmfUe, *amf_context.RanUe, amf_context.APIntentTransaction) bool {
				close(sendStarted)
				<-releaseSend
				return true
			},
		)
	}()
	<-sendStarted

	target := &amf_context.RanUe{
		AmfUeNgapId: 43,
		Ran:         &amf_context.AmfRan{AnType: models.AccessType__3_GPP_ACCESS},
	}
	switched := make(chan struct{})
	go func() {
		ue.AttachRanUe(target)
		source.DetachAmfUe()
		close(switched)
	}()
	select {
	case <-switched:
	case <-time.After(time.Second):
		t.Fatal("RanUe switch was blocked by AP response sender")
	}
	close(releaseSend)
	if result := <-resultCh; result != apIntentDeliveryAssociationChanged {
		t.Fatalf("delivery result=%v, want association changed", result)
	}
	current, ok := ue.CooperationContext.APIntent(transaction.PayloadID, transaction.Generation)
	if !ok || current.Status != amf_context.APIntentReady {
		t.Fatalf("transaction after stale send = %#v, ok=%v", current, ok)
	}

	var deliveredRanUe *amf_context.RanUe
	result := deliverReadyAPIntentFollowingAssociation(
		ue,
		transaction.PayloadID,
		transaction.Generation,
		true,
		func(_ *amf_context.AmfUe, ranUe *amf_context.RanUe, _ amf_context.APIntentTransaction) bool {
			deliveredRanUe = ranUe
			return true
		},
	)
	if result != apIntentDeliverySent || deliveredRanUe == nil || deliveredRanUe == target ||
		deliveredRanUe.AmfUeNgapId != target.AmfUeNgapId || deliveredRanUe.Ran != target.Ran {
		t.Fatalf("target retry result=%v ranUe=%p, want sent on %p", result, deliveredRanUe, target)
	}
}

func TestAPIntentReassemblesThroughMockHTTPAndFragmentsDL(t *testing.T) {
	server := httptest.NewServer(nagent.NewMockHandler(nagent.MockConfig{}))
	t.Cleanup(server.Close)
	client := nagent.NewClient(nagent.ClientConfig{
		BaseURI:         server.URL,
		ConnectTimeout:  time.Second,
		AttemptTimeout:  time.Second,
		TotalTimeout:    2 * time.Second,
		MaxAttempts:     1,
		MaxPayloadBytes: 65535,
	})
	dispatcher := nagent.NewDispatcher(stdcontext.Background(), client, 2, 4)
	t.Cleanup(dispatcher.Stop)

	type delivery struct {
		transaction amf_context.APIntentTransaction
		ies         []*nasMessage.CooperationIE
		err         error
	}
	delivered := make(chan delivery, 1)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		transaction amf_context.APIntentTransaction,
	) bool {
		ies, err := buildDLAPContainerIEs(transaction.MessageIdentity, &nasMessage.APContainer{
			ContainerType:      apIntentResponseContainerType,
			ContainerTypePTI:   transaction.ContainerTypePTI,
			ContainerPayloadID: transaction.PayloadID,
			Payload:            transaction.ResponsePayload,
		})
		delivered <- delivery{transaction: transaction, ies: ies, err: err}
		return err == nil
	})
	ue := testAPIntentUE(t)
	description := []byte(`{"intent":"` + strings.Repeat("x", 600) + `"}`)
	ulPayload := testIntentPayload(t, description)
	wantResponse := testAdaptedIntentPayload(t, ulPayload, ue.Supi)
	const fragmentSize = 200
	for offset := 0; offset < len(ulPayload); offset += fragmentSize {
		end := offset + fragmentSize
		if end > len(ulPayload) {
			end = len(ulPayload)
		}
		ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
		ul.MessageIdentity = 1
		if offset == 0 {
			mustAddIE(t, ul, 0x10, []byte{0x01})
		}
		mustAddAPContainerIE(t, ul, apFragment(
			0x3456, uint16(offset), end < len(ulPayload), ulPayload[offset:end],
		))
		dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
		if err != nil || len(dlMessages) != 0 {
			t.Fatalf("UL fragment offset=%d messages=%#v error=%v", offset, dlMessages, err)
		}
	}

	select {
	case got := <-delivered:
		if got.err != nil {
			t.Fatalf("buildDLAPContainerIEs() error = %v", got.err)
		}
		if !bytes.Equal(got.transaction.ResponsePayload, wantResponse) {
			t.Fatalf("mock NAgent response = %s, want %s", got.transaction.ResponsePayload, wantResponse)
		}
		if len(got.transaction.PendingDLIEs) != 1 || got.transaction.PendingDLIEs[0].IEI != 0x10 ||
			!bytes.Equal(got.transaction.PendingDLIEs[0].Contents, []byte{0x01}) {
			t.Fatalf("pending DL IEs were not retained across UL fragments: %#v",
				got.transaction.PendingDLIEs)
		}
		if len(got.ies) < 2 {
			t.Fatalf("DL AP Container count = %d, want fragmented response", len(got.ies))
		}
		reassembled := make([]byte, 0, len(wantResponse))
		for index, ie := range got.ies {
			fragment, err := nasMessage.DecodeAPContainer(ie.GetContents())
			if err != nil {
				t.Fatalf("DL fragment %d decode error = %v", index, err)
			}
			if int(fragment.FragmentOffset) != len(reassembled) ||
				fragment.MoreFragments() != (index < len(got.ies)-1) || fragment.DontFragment() ||
				fragment.ContainerType != apIntentResponseContainerType || fragment.ContainerTypePTI != 5 {
				t.Fatalf("DL fragment %d metadata = %#v", index, fragment)
			}
			reassembled = append(reassembled, fragment.Payload...)
		}
		if !bytes.Equal(reassembled, wantResponse) {
			t.Fatal("DL fragments do not reconstruct the mock response")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for mock NAgent response")
	}
}

func TestAPIntentAllowsParallelPayloadsToCompleteIndependently(t *testing.T) {
	firstReceived := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstReceivedOnce sync.Once
	var releaseFirstOnce sync.Once
	release := func() { releaseFirstOnce.Do(func() { close(releaseFirst) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		var forwarded nagent.Intent
		if err := json.Unmarshal(body, &forwarded); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if forwarded.IntentDescription == `{"id":1}` {
			firstReceivedOnce.Do(func() { close(firstReceived) })
			<-releaseFirst
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	client := nagent.NewClient(nagent.ClientConfig{
		BaseURI:         server.URL,
		ConnectTimeout:  time.Second,
		AttemptTimeout:  time.Second,
		TotalTimeout:    2 * time.Second,
		MaxAttempts:     1,
		MaxPayloadBytes: 65535,
	})
	dispatcher := nagent.NewDispatcher(stdcontext.Background(), client, 2, 4)
	t.Cleanup(dispatcher.Stop)
	t.Cleanup(release)
	delivered := make(chan amf_context.APIntentTransaction, 2)
	configureTestAPIntent(t, dispatcher, func(
		_ *amf_context.AmfUe,
		_ *amf_context.RanUe,
		transaction amf_context.APIntentTransaction,
	) bool {
		delivered <- transaction
		return true
	})
	ue := testAPIntentUE(t)

	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS,
		testAPIntentUL(t, 1, []byte(`{"id":1}`))); err != nil {
		t.Fatalf("first process error = %v", err)
	}
	select {
	case <-firstReceived:
	case <-time.After(time.Second):
		t.Fatal("first HTTP request did not start")
	}
	if _, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS,
		testAPIntentUL(t, 2, []byte(`{"id":2}`))); err != nil {
		t.Fatalf("second process error = %v", err)
	}

	select {
	case transaction := <-delivered:
		if transaction.PayloadID != 2 ||
			!bytes.Equal(transaction.ResponsePayload, testAdaptedIntentPayload(t, testIntentPayload(t, []byte(`{"id":2}`)), ue.Supi)) {
			t.Fatalf("first completed transaction = %#v, want payload 2", transaction)
		}
	case <-time.After(time.Second):
		t.Fatal("second payload was blocked by the first payload")
	}
	release()
	select {
	case transaction := <-delivered:
		if transaction.PayloadID != 1 ||
			!bytes.Equal(transaction.ResponsePayload, testAdaptedIntentPayload(t, testIntentPayload(t, []byte(`{"id":1}`)), ue.Supi)) {
			t.Fatalf("second completed transaction = %#v, want payload 1", transaction)
		}
	case <-time.After(time.Second):
		t.Fatal("first payload did not complete after release")
	}
}

func configureTestAPIntent(
	t *testing.T,
	dispatcher apIntentJobDispatcher,
	sender apIntentResponseSender,
) {
	t.Helper()
	configureAPIntentRuntime(apIntentRuntimeConfig{
		Enabled:    true,
		Dispatcher: dispatcher,
		DispatchUECallback: func(_ uint64, callback func()) bool {
			callback()
			return true
		},
		RequestTimeout:   2 * time.Second,
		ResponseTTL:      time.Minute,
		MaxInFlightPerUE: 8,
		Sender:           sender,
		AgentRoutes:       []nagent.AgentRoute{{Name: "default", Schema: "intent", IntentTypes: []string{"*"}}},
	})
	t.Cleanup(func() { configureAPIntentRuntime(apIntentRuntimeConfig{}) })
}

func testAPIntentUE(t *testing.T) *amf_context.AmfUe {
	t.Helper()
	ue := &amf_context.AmfUe{
		Supi:  "imsi-001010000000001",
		RanUe: map[models.AccessType]*amf_context.RanUe{},
		State: map[models.AccessType]*fsm.State{
			models.AccessType__3_GPP_ACCESS: fsm.NewState(amf_context.Registered),
		},
		NASLog:      logger.GmmLog,
		GmmLog:      logger.GmmLog,
		ProducerLog: logger.ProducerLog,
	}
	ue.AttachRanUe(&amf_context.RanUe{
		AmfUeNgapId: 42,
		Ran:         &amf_context.AmfRan{AnType: models.AccessType__3_GPP_ACCESS},
	})
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	return ue
}

func testAPIntentUL(t *testing.T, payloadID uint16, payload []byte) *nasMessage.ULCooperation {
	t.Helper()
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 1
	mustAddAPContainerIE(t, ul, &nasMessage.APContainer{
		ContainerType:      apIntentResponseContainerType,
		ContainerTypePTI:   5,
		ContainerPayloadID: payloadID,
		ContainerFlags:     nasMessage.APContainerFlagDF,
		Payload:            testIntentPayload(t, payload),
	})
	return ul
}

func testIntentPayload(t *testing.T, description []byte) []byte {
	t.Helper()
	payload, err := json.Marshal(nagent.Intent{
		IntentID:          "intent-test",
		Issuer:            "ue",
		IntentPriority:    10,
		IntentType:        "test",
		IntentDescription: string(description),
		Object:            "test-object",
		Constraint:        "",
		Target:            "test-target",
	})
	if err != nil {
		t.Fatalf("encode test Intent: %v", err)
	}
	return payload
}

func testAdaptedIntentPayload(t *testing.T, payload []byte, supi string) []byte {
	t.Helper()
	routes := []nagent.AgentRoute{{Name: "default", Schema: "intent", IntentTypes: []string{"*"}}}
	adapted, _, err := nagent.AdaptIntentPayload(payload, supi, routes)
	if err != nil {
		t.Fatalf("AdaptIntentPayload() error = %v", err)
	}
	return adapted
}

func readyAPIntentTransaction(
	t *testing.T,
	ue *amf_context.AmfUe,
	payloadID uint16,
) amf_context.APIntentTransaction {
	t.Helper()
	request := amf_context.APIntentRequest{
		PayloadID:        payloadID,
		MessageIdentity:  1,
		AccessType:       models.AccessType__3_GPP_ACCESS,
		ContainerType:    apIntentResponseContainerType,
		ContainerTypePTI: 5,
		Payload:          []byte(`{"intent":"test"}`),
	}
	request.RequestHash = nagent.IntentRequestFingerprint(nagent.IntentRequest{
		SUPI:            ue.Supi,
		AccessType:      string(request.AccessType),
		MessageIdentity: request.MessageIdentity,
		ContainerType:   request.ContainerType,
		PTI:             request.ContainerTypePTI,
		PayloadID:       request.PayloadID,
		Payload:         request.Payload,
	})
	_, transaction := ue.GetOrCreateCooperationContext().BeginAPIntent(request, time.Now())
	if !ue.CooperationContext.CompleteAPIntent(
		payloadID,
		transaction.Generation,
		[]byte(`{"result":"ok"}`),
		false,
		time.Now(),
		time.Minute,
	) {
		t.Fatal("CompleteAPIntent() = false")
	}
	ready, ok := ue.CooperationContext.APIntent(payloadID, transaction.Generation)
	if !ok {
		t.Fatal("ready transaction not found")
	}
	return ready
}

func assertAPIntentErrorCode(t *testing.T, messages [][]*nasMessage.CooperationIE, want string) {
	t.Helper()
	if len(messages) != 1 || len(messages[0]) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	ap, err := nasMessage.DecodeAPContainer(messages[0][0].GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(ap.Payload, &payload); err != nil {
		t.Fatalf("error payload is invalid JSON: %v", err)
	}
	if payload["$nagent"]["code"] != want {
		t.Fatalf("error payload = %s, want code %s", ap.Payload, want)
	}
}
