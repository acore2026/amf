package context

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/acore2026/openapi/models"
)

func TestAPIntentBeginDeduplicatesAndRejectsConflict(t *testing.T) {
	ctx := NewCooperationContext()
	now := time.Unix(100, 0)
	request := testAPIntentRequest(1, []byte(`{"intent":"one"}`))

	result, transaction := ctx.BeginAPIntent(request, now)
	if result != APIntentBeginNew || transaction.Generation == 0 {
		t.Fatalf("first BeginAPIntent() result=%v transaction=%#v", result, transaction)
	}
	result, duplicate := ctx.BeginAPIntent(request, now.Add(time.Second))
	if result != APIntentBeginPending || duplicate.Generation != transaction.Generation {
		t.Fatalf("duplicate BeginAPIntent() result=%v transaction=%#v", result, duplicate)
	}

	conflict := testAPIntentRequest(1, []byte(`{"intent":"different"}`))
	result, _ = ctx.BeginAPIntent(conflict, now.Add(2*time.Second))
	if result != APIntentBeginConflict {
		t.Fatalf("conflicting BeginAPIntent() result=%v, want conflict", result)
	}
}

func TestAPIntentCompletionCanBeReplayedAndIsDeepCopied(t *testing.T) {
	ctx := NewCooperationContext()
	now := time.Unix(100, 0)
	request := testAPIntentRequest(2, []byte(`{"intent":"two"}`))
	_, transaction := ctx.BeginAPIntent(request, now)
	response := []byte(`{"result":"ok"}`)

	if !ctx.CompleteAPIntent(2, transaction.Generation, response, false, now.Add(time.Second), time.Minute) {
		t.Fatal("CompleteAPIntent() = false")
	}
	response[0] = 'x'
	result, replay := ctx.BeginAPIntent(request, now.Add(2*time.Second))
	if result != APIntentBeginReplay || !bytes.Equal(replay.ResponsePayload, []byte(`{"result":"ok"}`)) {
		t.Fatalf("replay result=%v transaction=%#v", result, replay)
	}
	replay.ResponsePayload[0] = 'x'

	ready := ctx.ReadyAPIntents(models.AccessType__3_GPP_ACCESS, now.Add(2*time.Second))
	if len(ready) != 1 || !bytes.Equal(ready[0].ResponsePayload, []byte(`{"result":"ok"}`)) {
		t.Fatalf("ReadyAPIntents() = %#v", ready)
	}
	if !ctx.MarkAPIntentSent(2, transaction.Generation) {
		t.Fatal("MarkAPIntentSent() = false")
	}
}

func TestAPIntentStaleGenerationCannotCompleteReusedPayloadID(t *testing.T) {
	ctx := NewCooperationContext()
	now := time.Unix(100, 0)
	request := testAPIntentRequest(3, []byte(`{"intent":"old"}`))
	_, old := ctx.BeginAPIntent(request, now)
	ctx.RemoveAPIntent(3, old.Generation)

	newRequest := testAPIntentRequest(3, []byte(`{"intent":"new"}`))
	_, current := ctx.BeginAPIntent(newRequest, now.Add(time.Second))
	if current.Generation == old.Generation {
		t.Fatal("generation was reused")
	}
	if ctx.CompleteAPIntent(3, old.Generation, []byte(`{"stale":true}`), false,
		now.Add(2*time.Second), time.Minute) {
		t.Fatal("stale generation completed current transaction")
	}
}

func TestAPIntentEnforcesPerUELimit(t *testing.T) {
	ctx := NewCooperationContext()
	for id := uint16(1); id <= MaxAPIntentTransactionsPerUE; id++ {
		result, _ := ctx.BeginAPIntent(testAPIntentRequest(id, []byte(`{"ok":true}`)), time.Now())
		if result != APIntentBeginNew {
			t.Fatalf("payload %d result=%v", id, result)
		}
	}
	result, _ := ctx.BeginAPIntent(testAPIntentRequest(99, []byte(`{"overflow":true}`)), time.Now())
	if result != APIntentBeginLimit {
		t.Fatalf("overflow result=%v, want limit", result)
	}
}

func TestAPIntentHonorsConfiguredPerUELimit(t *testing.T) {
	ctx := NewCooperationContext()
	for id := uint16(1); id <= 2; id++ {
		result, _ := ctx.BeginAPIntentWithLimit(
			testAPIntentRequest(id, []byte(`{"ok":true}`)), time.Now(), 2,
		)
		if result != APIntentBeginNew {
			t.Fatalf("payload %d result=%v", id, result)
		}
	}
	result, _ := ctx.BeginAPIntentWithLimit(
		testAPIntentRequest(3, []byte(`{"overflow":true}`)), time.Now(), 2,
	)
	if result != APIntentBeginLimit {
		t.Fatalf("configured overflow result=%v, want limit", result)
	}
}

func TestStopAPContainerReassemblyTimersCancelsIntent(t *testing.T) {
	ue := &AmfUe{}
	ctx := ue.GetOrCreateCooperationContext()
	_, transaction := ctx.BeginAPIntent(testAPIntentRequest(4, []byte(`{"cancel":true}`)), time.Now())
	cancelled := make(chan struct{}, 1)
	if !ctx.SetAPIntentCancel(4, transaction.Generation, func() { cancelled <- struct{}{} }) {
		t.Fatal("SetAPIntentCancel() = false")
	}

	ue.StopAPContainerReassemblyTimers()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("intent cancel function was not called")
	}
}

func testAPIntentRequest(payloadID uint16, payload []byte) APIntentRequest {
	return APIntentRequest{
		PayloadID:        payloadID,
		RequestHash:      sha256.Sum256(payload),
		MessageIdentity:  1,
		AccessType:       models.AccessType__3_GPP_ACCESS,
		ContainerType:    0x0100,
		ContainerTypePTI: 5,
		Payload:          payload,
	}
}
