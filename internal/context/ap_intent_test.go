package context

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/acore2026/amf/internal/logger"
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

func TestAPIntentDuplicateMergesAndCopiesPendingDLIEs(t *testing.T) {
	ctx := NewCooperationContext()
	now := time.Unix(100, 0)
	request := testAPIntentRequest(31, []byte(`{"intent":"pending-dl"}`))
	request.PendingDLIEs = []DLCooperationIE{{IEI: 0x10, Contents: []byte{0x01}}}

	_, transaction := ctx.BeginAPIntent(request, now)
	request.PendingDLIEs[0].Contents[0] = 0xff

	duplicate := testAPIntentRequest(31, []byte(`{"intent":"pending-dl"}`))
	duplicate.PendingDLIEs = []DLCooperationIE{
		{IEI: 0x10, Contents: []byte{0x02}},
		{IEI: 0x11, Contents: []byte{0x03}},
	}
	result, merged := ctx.BeginAPIntent(duplicate, now.Add(time.Second))
	if result != APIntentBeginPending || merged.Generation != transaction.Generation {
		t.Fatalf("duplicate result=%v transaction=%#v", result, merged)
	}
	duplicate.PendingDLIEs[0].Contents[0] = 0xee
	if len(merged.PendingDLIEs) != 2 || merged.PendingDLIEs[0].IEI != 0x10 ||
		!bytes.Equal(merged.PendingDLIEs[0].Contents, []byte{0x02}) ||
		merged.PendingDLIEs[1].IEI != 0x11 ||
		!bytes.Equal(merged.PendingDLIEs[1].Contents, []byte{0x03}) {
		t.Fatalf("merged pending DL IEs = %#v", merged.PendingDLIEs)
	}

	stored, ok := ctx.APIntent(31, transaction.Generation)
	if !ok || !bytes.Equal(stored.PendingDLIEs[0].Contents, []byte{0x02}) {
		t.Fatalf("stored pending DL IEs = %#v, ok=%v", stored.PendingDLIEs, ok)
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

func TestAPIntentDeliveryClaimSerializesSendAndCanReturnToReady(t *testing.T) {
	ctx := NewCooperationContext()
	now := time.Now()
	request := testAPIntentRequest(20, []byte(`{"intent":"claim"}`))
	_, transaction := ctx.BeginAPIntent(request, now)
	if !ctx.CompleteAPIntent(
		request.PayloadID, transaction.Generation, []byte(`{"result":"ok"}`), false,
		now.Add(time.Second), time.Minute,
	) {
		t.Fatal("CompleteAPIntent() = false")
	}

	claimed, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation)
	if !ok || claimed.Status != APIntentSending {
		t.Fatalf("first claim = %#v, ok=%v", claimed, ok)
	}
	if _, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation); ok {
		t.Fatal("second concurrent delivery claim succeeded")
	}
	if result, _ := ctx.BeginAPIntent(request, now.Add(2*time.Second)); result != APIntentBeginPending {
		t.Fatalf("duplicate while sending result=%v, want pending", result)
	}
	if !ctx.FinishAPIntentDelivery(request.PayloadID, transaction.Generation, false) {
		t.Fatal("failed delivery did not return transaction to ready")
	}
	retry, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation)
	if !ok || retry.Status != APIntentSending {
		t.Fatalf("retry claim = %#v, ok=%v", retry, ok)
	}
	if !ctx.FinishAPIntentDelivery(request.PayloadID, transaction.Generation, true) {
		t.Fatal("successful delivery did not finish")
	}
	current, ok := ctx.APIntent(request.PayloadID, transaction.Generation)
	if !ok || current.Status != APIntentSent {
		t.Fatalf("final transaction = %#v, ok=%v", current, ok)
	}
}

func TestAPIntentExpiryDoesNotRemoveActiveDelivery(t *testing.T) {
	ctx := NewCooperationContext()
	request := testAPIntentRequest(23, []byte(`{"intent":"slow-send"}`))
	_, transaction := ctx.BeginAPIntent(request, time.Now())
	if !ctx.CompleteAPIntent(
		request.PayloadID, transaction.Generation, []byte(`{"result":"ok"}`), false,
		time.Now(), 20*time.Millisecond,
	) {
		t.Fatal("CompleteAPIntent() = false")
	}
	if _, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation); !ok {
		t.Fatal("ClaimAPIntentDelivery() = false")
	}
	time.Sleep(30 * time.Millisecond)
	active, ok := ctx.APIntent(request.PayloadID, transaction.Generation)
	if !ok || active.Status != APIntentSending {
		t.Fatalf("active delivery expired: %#v, ok=%v", active, ok)
	}
	if result, _ := ctx.BeginAPIntent(request, time.Now()); result != APIntentBeginPending {
		t.Fatalf("duplicate during expired send result=%v, want pending", result)
	}
	_ = ctx.ReadyAPIntents(request.AccessType, time.Now())
	if _, ok := ctx.APIntent(request.PayloadID, transaction.Generation); !ok {
		t.Fatal("ReadyAPIntents removed an active delivery")
	}
	if !ctx.FinishAPIntentDelivery(request.PayloadID, transaction.Generation, false) {
		t.Fatal("FinishAPIntentDelivery() = false")
	}
	if _, ok := ctx.APIntent(request.PayloadID, transaction.Generation); ok {
		t.Fatal("expired failed delivery returned to Ready")
	}
}

func TestAPIntentExpiryCallbackDoesNotRemoveSendingTransaction(t *testing.T) {
	ctx := NewCooperationContext()
	request := testAPIntentRequest(24, []byte(`{"intent":"timer-race"}`))
	_, transaction := ctx.BeginAPIntent(request, time.Now())
	if !ctx.CompleteAPIntent(
		request.PayloadID, transaction.Generation, []byte(`{"result":"ok"}`), false,
		time.Now(), time.Minute,
	) {
		t.Fatal("CompleteAPIntent() = false")
	}
	if _, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation); !ok {
		t.Fatal("ClaimAPIntentDelivery() = false")
	}
	if ctx.expireAPIntent(request.PayloadID, transaction.Generation, time.Now().Add(time.Hour)) {
		t.Fatal("expiry callback removed a Sending transaction")
	}
	active, ok := ctx.APIntent(request.PayloadID, transaction.Generation)
	if !ok || active.Status != APIntentSending {
		t.Fatalf("active transaction = %#v, ok=%v", active, ok)
	}
}

func TestAPDeliverySnapshotIsMinimalAndTracksAssociation(t *testing.T) {
	accessType := models.AccessType__3_GPP_ACCESS
	ue := &AmfUe{
		RanUe:       make(map[models.AccessType]*RanUe),
		NASLog:      logger.GmmLog,
		GmmLog:      logger.GmmLog,
		ProducerLog: logger.ProducerLog,
	}
	source := &RanUe{
		RanUeNgapId: 11,
		AmfUeNgapId: 22,
		AmfUe:       ue,
		Ran:         &AmfRan{AnType: accessType},
		OldAmfName:  "old-amf",
	}
	ue.AttachRanUe(source)

	actual, target, generation, ok := ue.APDeliveryRanUeSnapshot(accessType, source)
	if !ok || actual != source || generation == 0 {
		t.Fatalf("snapshot actual=%p generation=%d ok=%v", actual, generation, ok)
	}
	if target == source || target.AmfUeNgapId != source.AmfUeNgapId ||
		target.RanUeNgapId != source.RanUeNgapId || target.OldAmfName != "" {
		t.Fatalf("delivery target = %#v", target)
	}
	if source.OldAmfName != "old-amf" || !ue.APDeliveryTargetCurrent(accessType, target) {
		t.Fatal("snapshot consumed OldAmfName or lost its association")
	}

	ue.ranUeMu.Lock()
	ue.advanceAPDeliveryGenerationLocked(accessType)
	ue.ranUeMu.Unlock()
	if ue.APDeliveryTargetCurrent(accessType, target) {
		t.Fatal("stale delivery target remained current")
	}
}

func TestRanUeAssociationSupportsConcurrentReadersAndWriter(t *testing.T) {
	accessType := models.AccessType__3_GPP_ACCESS
	ue := &AmfUe{
		RanUe:       make(map[models.AccessType]*RanUe),
		NASLog:      logger.GmmLog,
		GmmLog:      logger.GmmLog,
		ProducerLog: logger.ProducerLog,
	}
	first := &RanUe{Ran: &AmfRan{AnType: accessType}, Log: logger.NgapLog}
	second := &RanUe{Ran: &AmfRan{AnType: accessType}, Log: logger.NgapLog}
	ue.AttachRanUe(first)

	const iterations = 1000
	start := make(chan struct{})
	errors := make(chan string, 1)
	report := func(message string) {
		select {
		case errors <- message:
		default:
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for index := 0; index < iterations; index++ {
			if index%2 == 0 {
				ue.AttachRanUe(second)
			} else {
				ue.AttachRanUe(first)
			}
		}
	}()
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for index := 0; index < iterations; index++ {
				ranUe := ue.RanUeForAccessType(accessType)
				if ranUe != first && ranUe != second {
					report("accessor returned an unexpected association")
					return
				}
				if snapshot := ue.RanUeSnapshot(); snapshot[accessType] == nil {
					report("snapshot omitted the active association")
					return
				}
				if _, _, _, ok := ue.APDeliveryRanUeSnapshot(accessType, nil); !ok {
					report("delivery snapshot rejected the active association")
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	select {
	case message := <-errors:
		t.Fatal(message)
	default:
	}
}

func TestAPIntentNASNonDeliveryMatchesOnlyCurrentDeliveryAttempt(t *testing.T) {
	ctx := NewCooperationContext()
	request := testAPIntentRequest(25, []byte(`{"intent":"non-delivery"}`))
	_, transaction := ctx.BeginAPIntent(request, time.Now())
	if !ctx.CompleteAPIntent(
		request.PayloadID, transaction.Generation, []byte(`{"result":"ok"}`), false,
		time.Now(), time.Minute,
	) {
		t.Fatal("CompleteAPIntent() = false")
	}

	first, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation)
	if !ok || first.DeliveryAttempt == 0 {
		t.Fatalf("first claim = %#v, ok=%v", first, ok)
	}
	firstPDU := []byte{0x7e, 0x02, 0x01, 0x02, 0x03}
	if !ctx.RecordAPIntentDLNAS(request.PayloadID, transaction.Generation, first.DeliveryAttempt, firstPDU) {
		t.Fatal("RecordAPIntentDLNAS(first) = false")
	}
	status, ok := ctx.FinishAPIntentDeliveryAttempt(
		request.PayloadID, transaction.Generation, first.DeliveryAttempt, true,
	)
	if !ok || status != APIntentSent {
		t.Fatalf("first finish status=%v ok=%v", status, ok)
	}
	if _, matched := ctx.MarkAPIntentNASNonDelivery(request.AccessType, []byte{0x00}); matched {
		t.Fatal("unrelated NAS PDU matched a delivery")
	}
	matchedTransaction, matched := ctx.MarkAPIntentNASNonDelivery(request.AccessType, firstPDU)
	if !matched || matchedTransaction.Status != APIntentReady {
		t.Fatalf("matched transaction=%#v matched=%v", matchedTransaction, matched)
	}
	if _, matched = ctx.MarkAPIntentNASNonDelivery(request.AccessType, firstPDU); matched {
		t.Fatal("duplicate NAS Non-Delivery matched twice")
	}

	second, ok := ctx.ClaimAPIntentDelivery(request.PayloadID, transaction.Generation)
	if !ok || second.DeliveryAttempt <= first.DeliveryAttempt {
		t.Fatalf("second claim = %#v, ok=%v", second, ok)
	}
	secondPDU := []byte{0x7e, 0x02, 0x04, 0x05, 0x06}
	if !ctx.RecordAPIntentDLNAS(request.PayloadID, transaction.Generation, second.DeliveryAttempt, secondPDU) {
		t.Fatal("RecordAPIntentDLNAS(second) = false")
	}
	if _, matched = ctx.MarkAPIntentNASNonDelivery(request.AccessType, firstPDU); matched {
		t.Fatal("stale delivery attempt matched the current transaction")
	}
	matchedTransaction, matched = ctx.MarkAPIntentNASNonDelivery(request.AccessType, secondPDU)
	if !matched || matchedTransaction.Status != APIntentSending {
		t.Fatalf("in-flight match transaction=%#v matched=%v", matchedTransaction, matched)
	}
	status, ok = ctx.FinishAPIntentDeliveryAttempt(
		request.PayloadID, transaction.Generation, second.DeliveryAttempt, true,
	)
	if !ok || status != APIntentReady {
		t.Fatalf("finish after in-flight Non-Delivery status=%v ok=%v", status, ok)
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
