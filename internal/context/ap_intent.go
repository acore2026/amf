package context

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sort"
	"time"

	"github.com/acore2026/openapi/models"
)

const MaxAPIntentTransactionsPerUE uint16 = 8

type APIntentStatus uint8

const (
	APIntentPending APIntentStatus = iota + 1
	APIntentReady
	APIntentSending
	APIntentSent // All DL fragments were written to SCTP; UE receipt is not acknowledged.
)

type APIntentBeginResult uint8

const (
	APIntentBeginNew APIntentBeginResult = iota + 1
	APIntentBeginPending
	APIntentBeginReplay
	APIntentBeginConflict
	APIntentBeginLimit
)

type APIntentRequest struct {
	PayloadID        uint16
	RequestHash      [32]byte
	HTTPRequestID    string
	MessageIdentity  uint8
	AccessType       models.AccessType
	ContainerType    uint16
	ContainerTypePTI uint8
	Payload          []byte
	PendingDLIEs     []DLCooperationIE
}

type APIntentTransaction struct {
	APIntentRequest
	Generation            uint64
	DeliveryAttempt       uint64
	Status                APIntentStatus
	ResponsePayload       []byte
	ResponseIsError       bool
	CreatedAt             time.Time
	CompletedAt           time.Time
	ExpiresAt             time.Time
	Cancel                context.CancelFunc
	Timer                 *time.Timer
	failedDeliveryAttempt uint64
	deliveryPDUHashes     map[[32]byte]struct{}
}

type APIntentDLDeliveryRef struct {
	PayloadID  uint16
	Generation uint64
	Attempt    uint64
}

func (c *CooperationContext) BeginAPIntent(
	request APIntentRequest,
	now time.Time,
) (APIntentBeginResult, APIntentTransaction) {
	return c.BeginAPIntentWithLimit(request, now, int(MaxAPIntentTransactionsPerUE))
}

func (c *CooperationContext) BeginAPIntentWithLimit(
	request APIntentRequest,
	now time.Time,
	maxTransactions int,
) (APIntentBeginResult, APIntentTransaction) {
	if maxTransactions <= 0 || maxTransactions > int(MaxAPIntentTransactionsPerUE) {
		maxTransactions = int(MaxAPIntentTransactionsPerUE)
	}
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()

	if existing := state.IntentTransactions[request.PayloadID]; existing != nil {
		if existing.Status != APIntentSending &&
			!existing.ExpiresAt.IsZero() && !now.Before(existing.ExpiresAt) {
			removeAPIntentLocked(state, request.PayloadID, existing.Generation)
		} else if !bytes.Equal(existing.RequestHash[:], request.RequestHash[:]) {
			return APIntentBeginConflict, cloneAPIntentTransaction(existing)
		} else {
			if existing.Status == APIntentPending {
				mergePendingDLCooperationIEs(existing, request.PendingDLIEs)
				return APIntentBeginPending, cloneAPIntentTransaction(existing)
			}
			if existing.Status == APIntentSending {
				return APIntentBeginPending, cloneAPIntentTransaction(existing)
			}
			mergePendingDLCooperationIEs(existing, request.PendingDLIEs)
			return APIntentBeginReplay, cloneAPIntentTransaction(existing)
		}
	}

	ensureAPIntentCapacityLocked(state, now, maxTransactions)
	if len(state.IntentTransactions) >= maxTransactions {
		return APIntentBeginLimit, APIntentTransaction{}
	}
	state.NextIntentGeneration++
	transaction := &APIntentTransaction{
		APIntentRequest: cloneAPIntentRequest(request),
		Generation:      state.NextIntentGeneration,
		Status:          APIntentPending,
		CreatedAt:       now,
	}
	state.IntentTransactions[request.PayloadID] = transaction
	return APIntentBeginNew, cloneAPIntentTransaction(transaction)
}

func (c *CooperationContext) SetAPIntentCancel(
	payloadID uint16,
	generation uint64,
	cancel context.CancelFunc,
) bool {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation || transaction.Status != APIntentPending {
		return false
	}
	transaction.Cancel = cancel
	return true
}

func (c *CooperationContext) CompleteAPIntent(
	payloadID uint16,
	generation uint64,
	response []byte,
	isError bool,
	now time.Time,
	ttl time.Duration,
) bool {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation || transaction.Status != APIntentPending {
		return false
	}
	transaction.Status = APIntentReady
	transaction.ResponsePayload = append([]byte(nil), response...)
	transaction.ResponseIsError = isError
	transaction.CompletedAt = now
	transaction.ExpiresAt = now.Add(ttl)
	transaction.Cancel = nil
	scheduleAPIntentExpiryLocked(c, transaction, ttl)
	return true
}

func (c *CooperationContext) ReadyAPIntents(
	accessType models.AccessType,
	now time.Time,
) []APIntentTransaction {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	result := make([]APIntentTransaction, 0)
	for payloadID, transaction := range state.IntentTransactions {
		if transaction.Status != APIntentSending &&
			!transaction.ExpiresAt.IsZero() && !now.Before(transaction.ExpiresAt) {
			removeAPIntentLocked(state, payloadID, transaction.Generation)
			continue
		}
		if transaction.Status == APIntentReady && transaction.AccessType == accessType {
			result = append(result, cloneAPIntentTransaction(transaction))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CompletedAt.Before(result[j].CompletedAt)
	})
	return result
}

func (c *CooperationContext) MarkAPIntentSent(payloadID uint16, generation uint64) bool {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation || transaction.Status != APIntentReady {
		return false
	}
	transaction.Status = APIntentSent
	return true
}

func (c *CooperationContext) PrepareAPIntentReplay(payloadID uint16, generation uint64) bool {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation {
		return false
	}
	if !transaction.ExpiresAt.IsZero() && !time.Now().Before(transaction.ExpiresAt) {
		removeAPIntentLocked(state, payloadID, generation)
		return false
	}
	switch transaction.Status {
	case APIntentSent:
		clearAPIntentDLDeliveriesLocked(state, transaction)
		transaction.Status = APIntentReady
		return true
	case APIntentReady:
		return true
	default:
		return false
	}
}

func (c *CooperationContext) ClaimAPIntentDelivery(
	payloadID uint16,
	generation uint64,
) (APIntentTransaction, bool) {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation || transaction.Status != APIntentReady {
		return APIntentTransaction{}, false
	}
	if !transaction.ExpiresAt.IsZero() && !time.Now().Before(transaction.ExpiresAt) {
		removeAPIntentLocked(state, payloadID, generation)
		return APIntentTransaction{}, false
	}
	if transaction.Timer != nil {
		transaction.Timer.Stop()
		transaction.Timer = nil
	}
	clearAPIntentDLDeliveriesLocked(state, transaction)
	transaction.DeliveryAttempt++
	transaction.failedDeliveryAttempt = 0
	transaction.Status = APIntentSending
	return cloneAPIntentTransaction(transaction), true
}

func (c *CooperationContext) FinishAPIntentDelivery(
	payloadID uint16,
	generation uint64,
	sent bool,
) bool {
	_, finished := c.FinishAPIntentDeliveryAttempt(payloadID, generation, 0, sent)
	return finished
}

func (c *CooperationContext) FinishAPIntentDeliveryAttempt(
	payloadID uint16,
	generation uint64,
	attempt uint64,
	sent bool,
) (APIntentStatus, bool) {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation || transaction.Status != APIntentSending ||
		(attempt != 0 && transaction.DeliveryAttempt != attempt) {
		return 0, false
	}
	if sent && transaction.failedDeliveryAttempt != transaction.DeliveryAttempt {
		transaction.Status = APIntentSent
	} else {
		transaction.Status = APIntentReady
		clearAPIntentDLDeliveriesLocked(state, transaction)
	}
	status := transaction.Status
	if !transaction.ExpiresAt.IsZero() {
		remaining := time.Until(transaction.ExpiresAt)
		if remaining <= 0 {
			removeAPIntentLocked(state, payloadID, generation)
			return status, true
		}
		scheduleAPIntentExpiryLocked(c, transaction, remaining)
	}
	return status, true
}

func (c *CooperationContext) RecordAPIntentDLNAS(
	payloadID uint16,
	generation uint64,
	attempt uint64,
	nasPDU []byte,
) bool {
	if len(nasPDU) == 0 || attempt == 0 {
		return false
	}
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation ||
		transaction.DeliveryAttempt != attempt || transaction.Status != APIntentSending {
		return false
	}
	if state.IntentDLDeliveries == nil {
		state.IntentDLDeliveries = make(map[[32]byte]APIntentDLDeliveryRef)
	}
	hash := sha256.Sum256(nasPDU)
	state.IntentDLDeliveries[hash] = APIntentDLDeliveryRef{
		PayloadID: payloadID, Generation: generation, Attempt: attempt,
	}
	if transaction.deliveryPDUHashes == nil {
		transaction.deliveryPDUHashes = make(map[[32]byte]struct{})
	}
	transaction.deliveryPDUHashes[hash] = struct{}{}
	return true
}

func (c *CooperationContext) MarkAPIntentNASNonDelivery(
	accessType models.AccessType,
	nasPDU []byte,
) (APIntentTransaction, bool) {
	if len(nasPDU) == 0 {
		return APIntentTransaction{}, false
	}
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	ref, ok := state.IntentDLDeliveries[sha256.Sum256(nasPDU)]
	if !ok {
		return APIntentTransaction{}, false
	}
	transaction := state.IntentTransactions[ref.PayloadID]
	if transaction == nil || transaction.Generation != ref.Generation ||
		transaction.DeliveryAttempt != ref.Attempt || transaction.AccessType != accessType ||
		(transaction.Status != APIntentSending && transaction.Status != APIntentSent) {
		return APIntentTransaction{}, false
	}
	transaction.failedDeliveryAttempt = ref.Attempt
	clearAPIntentDLDeliveriesLocked(state, transaction)
	if transaction.Status == APIntentSent {
		transaction.Status = APIntentReady
	}
	return cloneAPIntentTransaction(transaction), true
}

func scheduleAPIntentExpiryLocked(
	c *CooperationContext,
	transaction *APIntentTransaction,
	after time.Duration,
) {
	payloadID := transaction.PayloadID
	generation := transaction.Generation
	transaction.Timer = time.AfterFunc(after, func() {
		c.expireAPIntent(payloadID, generation, time.Now())
	})
}

func (c *CooperationContext) expireAPIntent(
	payloadID uint16,
	generation uint64,
	now time.Time,
) bool {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation ||
		transaction.Status == APIntentSending || transaction.ExpiresAt.IsZero() ||
		now.Before(transaction.ExpiresAt) {
		return false
	}
	return removeAPIntentLocked(state, payloadID, generation)
}

func (c *CooperationContext) APIntent(
	payloadID uint16,
	generation uint64,
) (APIntentTransaction, bool) {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation {
		return APIntentTransaction{}, false
	}
	return cloneAPIntentTransaction(transaction), true
}

func (c *CooperationContext) RemoveAPIntent(payloadID uint16, generation uint64) bool {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	return removeAPIntentLocked(state, payloadID, generation)
}

func removeAPIntentLocked(state *APContainerState, payloadID uint16, generation uint64) bool {
	transaction := state.IntentTransactions[payloadID]
	if transaction == nil || transaction.Generation != generation {
		return false
	}
	if transaction.Cancel != nil {
		transaction.Cancel()
	}
	if transaction.Timer != nil {
		transaction.Timer.Stop()
	}
	clearAPIntentDLDeliveriesLocked(state, transaction)
	delete(state.IntentTransactions, payloadID)
	return true
}

func clearAPIntentDLDeliveriesLocked(state *APContainerState, transaction *APIntentTransaction) {
	for hash := range transaction.deliveryPDUHashes {
		ref, ok := state.IntentDLDeliveries[hash]
		if ok && ref.PayloadID == transaction.PayloadID && ref.Generation == transaction.Generation &&
			ref.Attempt == transaction.DeliveryAttempt {
			delete(state.IntentDLDeliveries, hash)
		}
	}
	transaction.deliveryPDUHashes = nil
}

func ensureAPIntentCapacityLocked(state *APContainerState, now time.Time, maxTransactions int) {
	for payloadID, transaction := range state.IntentTransactions {
		if transaction.Status != APIntentSending &&
			!transaction.ExpiresAt.IsZero() && !now.Before(transaction.ExpiresAt) {
			removeAPIntentLocked(state, payloadID, transaction.Generation)
		}
	}
	for len(state.IntentTransactions) >= maxTransactions {
		var oldest *APIntentTransaction
		for _, transaction := range state.IntentTransactions {
			if transaction.Status != APIntentSent {
				continue
			}
			if oldest == nil || transaction.CompletedAt.Before(oldest.CompletedAt) {
				oldest = transaction
			}
		}
		if oldest == nil {
			return
		}
		removeAPIntentLocked(state, oldest.PayloadID, oldest.Generation)
	}
}

func cloneAPIntentRequest(request APIntentRequest) APIntentRequest {
	request.Payload = append([]byte(nil), request.Payload...)
	request.PendingDLIEs = cloneDLCooperationIEs(request.PendingDLIEs)
	return request
}

func mergePendingDLCooperationIEs(transaction *APIntentTransaction, incoming []DLCooperationIE) {
	for _, candidate := range incoming {
		replaced := false
		for index := range transaction.PendingDLIEs {
			if transaction.PendingDLIEs[index].IEI != candidate.IEI {
				continue
			}
			transaction.PendingDLIEs[index].Contents = append([]byte(nil), candidate.Contents...)
			replaced = true
			break
		}
		if !replaced {
			transaction.PendingDLIEs = append(transaction.PendingDLIEs, DLCooperationIE{
				IEI:      candidate.IEI,
				Contents: append([]byte(nil), candidate.Contents...),
			})
		}
	}
}

func cloneDLCooperationIEs(ies []DLCooperationIE) []DLCooperationIE {
	if len(ies) == 0 {
		return nil
	}
	result := make([]DLCooperationIE, len(ies))
	for index, ie := range ies {
		result[index] = DLCooperationIE{
			IEI:      ie.IEI,
			Contents: append([]byte(nil), ie.Contents...),
		}
	}
	return result
}

func cloneAPIntentTransaction(transaction *APIntentTransaction) APIntentTransaction {
	if transaction == nil {
		return APIntentTransaction{}
	}
	result := *transaction
	result.APIntentRequest = cloneAPIntentRequest(transaction.APIntentRequest)
	result.ResponsePayload = append([]byte(nil), transaction.ResponsePayload...)
	result.Cancel = nil
	result.Timer = nil
	result.deliveryPDUHashes = nil
	return result
}
