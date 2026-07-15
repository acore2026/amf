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
	"github.com/acore2026/amf/internal/nagent"
	"github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
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

func TestAPIntentSendsOrdinaryIEImmediatelyAndDeliversEchoLater(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan amf_context.APIntentTransaction, 1)
	configureTestAPIntent(t, dispatcher, func(_ *amf_context.AmfUe, transaction amf_context.APIntentTransaction) bool {
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
	if len(dlMessages) != 1 || len(dlMessages[0]) != 1 {
		t.Fatalf("immediate DL messages = %#v, want only IE 0x10", dlMessages)
	}
	assertDLIE(t, dlMessages[0][0], 0x10, []byte{0x01})
	jobs := dispatcher.Jobs()
	if len(jobs) != 1 || string(jobs[0].Request.Payload) != string(payload) {
		t.Fatalf("jobs = %#v", jobs)
	}

	jobs[0].Callback(nagent.Result{Request: jobs[0].Request, Response: payload})
	select {
	case transaction := <-delivered:
		if transaction.PayloadID != 0x1234 || transaction.ContainerType != 0x0100 ||
			transaction.ContainerTypePTI != 5 || transaction.MessageIdentity != 1 ||
			string(transaction.ResponsePayload) != string(payload) || transaction.ResponseIsError {
			t.Fatalf("delivered transaction = %#v", transaction)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP result was not delivered")
	}
}

func TestAPIntentPendingDuplicateDoesNotSubmitAgainAndConflictReturnsError(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	configureTestAPIntent(t, dispatcher, func(*amf_context.AmfUe, amf_context.APIntentTransaction) bool {
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

func TestAPIntentHTTPErrorBecomesCanonicalDLJSON(t *testing.T) {
	dispatcher := &fakeAPIntentDispatcher{}
	delivered := make(chan amf_context.APIntentTransaction, 1)
	configureTestAPIntent(t, dispatcher, func(_ *amf_context.AmfUe, transaction amf_context.APIntentTransaction) bool {
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
	configureTestAPIntent(t, dispatcher, func(ue *amf_context.AmfUe, _ amf_context.APIntentTransaction) bool {
		if ue.RanUe[models.AccessType__3_GPP_ACCESS] == nil {
			return false
		}
		deliveries++
		return true
	})
	ue := testAPIntentUE(t)
	ul := testAPIntentUL(t, 11, []byte(`{"intent":"offline"}`))
	_, _ = processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	oldRanUe := ue.RanUe[models.AccessType__3_GPP_ACCESS]
	oldRanUe.DetachAmfUe()
	delete(ue.RanUe, models.AccessType__3_GPP_ACCESS)
	job := dispatcher.Jobs()[0]
	job.Callback(nagent.Result{Request: job.Request, Response: []byte(`{"result":"ready"}`)})
	if deliveries != 0 {
		t.Fatal("response was delivered while UE was offline")
	}

	ue.RanUe[models.AccessType__3_GPP_ACCESS] = &amf_context.RanUe{AmfUeNgapId: 42, AmfUe: ue}
	sendPendingAPIntentResponses(ue, models.AccessType__3_GPP_ACCESS)
	if deliveries != 1 {
		t.Fatalf("deliveries = %d, want 1", deliveries)
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
	configureTestAPIntent(t, dispatcher, func(_ *amf_context.AmfUe, transaction amf_context.APIntentTransaction) bool {
		ies, err := buildDLAPContainerIEs(transaction.MessageIdentity, &nasMessage.APContainer{
			ContainerType:      transaction.ContainerType,
			ContainerTypePTI:   transaction.ContainerTypePTI,
			ContainerPayloadID: transaction.PayloadID,
			Payload:            transaction.ResponsePayload,
		})
		delivered <- delivery{transaction: transaction, ies: ies, err: err}
		return err == nil
	})
	ue := testAPIntentUE(t)
	payload := []byte(`{"intent":"` + strings.Repeat("x", 600) + `"}`)
	const fragmentSize = 200
	for offset := 0; offset < len(payload); offset += fragmentSize {
		end := offset + fragmentSize
		if end > len(payload) {
			end = len(payload)
		}
		ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
		ul.MessageIdentity = 1
		mustAddAPContainerIE(t, ul, apFragment(0x3456, uint16(offset), end < len(payload), payload[offset:end]))
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
		if !bytes.Equal(got.transaction.ResponsePayload, payload) {
			t.Fatal("mock NAgent did not echo the reassembled payload")
		}
		if len(got.ies) < 2 {
			t.Fatalf("DL AP Container count = %d, want fragmented response", len(got.ies))
		}
		reassembled := make([]byte, 0, len(payload))
		for index, ie := range got.ies {
			fragment, err := nasMessage.DecodeAPContainer(ie.GetContents())
			if err != nil {
				t.Fatalf("DL fragment %d decode error = %v", index, err)
			}
			if int(fragment.FragmentOffset) != len(reassembled) ||
				fragment.MoreFragments() != (index < len(got.ies)-1) || fragment.DontFragment() {
				t.Fatalf("DL fragment %d metadata = %#v", index, fragment)
			}
			reassembled = append(reassembled, fragment.Payload...)
		}
		if !bytes.Equal(reassembled, payload) {
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
		if bytes.Contains(body, []byte(`"id":1`)) {
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
	configureTestAPIntent(t, dispatcher, func(_ *amf_context.AmfUe, transaction amf_context.APIntentTransaction) bool {
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
		if transaction.PayloadID != 2 || !bytes.Equal(transaction.ResponsePayload, []byte(`{"id":2}`)) {
			t.Fatalf("first completed transaction = %#v, want payload 2", transaction)
		}
	case <-time.After(time.Second):
		t.Fatal("second payload was blocked by the first payload")
	}
	release()
	select {
	case transaction := <-delivered:
		if transaction.PayloadID != 1 || !bytes.Equal(transaction.ResponsePayload, []byte(`{"id":1}`)) {
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
		ResponseTTL:      time.Minute,
		MaxInFlightPerUE: 8,
		Sender:           sender,
	})
	t.Cleanup(func() { configureAPIntentRuntime(apIntentRuntimeConfig{}) })
}

func testAPIntentUE(t *testing.T) *amf_context.AmfUe {
	t.Helper()
	ue := &amf_context.AmfUe{
		Supi:  "imsi-001010000000001",
		RanUe: map[models.AccessType]*amf_context.RanUe{},
	}
	ue.RanUe[models.AccessType__3_GPP_ACCESS] = &amf_context.RanUe{AmfUeNgapId: 42, AmfUe: ue}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	return ue
}

func testAPIntentUL(t *testing.T, payloadID uint16, payload []byte) *nasMessage.ULCooperation {
	t.Helper()
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 1
	mustAddAPContainerIE(t, ul, &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   5,
		ContainerPayloadID: payloadID,
		ContainerFlags:     nasMessage.APContainerFlagDF,
		Payload:            payload,
	})
	return ul
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
