package context

import (
	"bytes"
	"context"
	"sort"
	"time"

	"github.com/acore2026/openapi/models"
)

const MaxAPIntentTransactionsPerUE uint16 = 8

type APIntentStatus uint8

const (
	APIntentPending APIntentStatus = iota + 1
	APIntentReady
	APIntentSent
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
	MessageIdentity  uint8
	AccessType       models.AccessType
	ContainerType    uint16
	ContainerTypePTI uint8
	Payload          []byte
}

type APIntentTransaction struct {
	APIntentRequest
	Generation      uint64
	Status          APIntentStatus
	ResponsePayload []byte
	ResponseIsError bool
	CreatedAt       time.Time
	CompletedAt     time.Time
	ExpiresAt       time.Time
	Cancel          context.CancelFunc
	Timer           *time.Timer
}

func (c *CooperationContext) BeginAPIntent(
	request APIntentRequest,
	now time.Time,
) (APIntentBeginResult, APIntentTransaction) {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()

	if existing := state.IntentTransactions[request.PayloadID]; existing != nil {
		if !existing.ExpiresAt.IsZero() && !now.Before(existing.ExpiresAt) {
			removeAPIntentLocked(state, request.PayloadID, existing.Generation)
		} else if !bytes.Equal(existing.RequestHash[:], request.RequestHash[:]) {
			return APIntentBeginConflict, cloneAPIntentTransaction(existing)
		} else if existing.Status == APIntentPending {
			return APIntentBeginPending, cloneAPIntentTransaction(existing)
		} else {
			return APIntentBeginReplay, cloneAPIntentTransaction(existing)
		}
	}

	ensureAPIntentCapacityLocked(state, now)
	if len(state.IntentTransactions) >= int(MaxAPIntentTransactionsPerUE) {
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
	transaction.Timer = time.AfterFunc(ttl, func() {
		c.RemoveAPIntent(payloadID, generation)
	})
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
		if !transaction.ExpiresAt.IsZero() && !now.Before(transaction.ExpiresAt) {
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
	delete(state.IntentTransactions, payloadID)
	return true
}

func ensureAPIntentCapacityLocked(state *APContainerState, now time.Time) {
	for payloadID, transaction := range state.IntentTransactions {
		if !transaction.ExpiresAt.IsZero() && !now.Before(transaction.ExpiresAt) {
			removeAPIntentLocked(state, payloadID, transaction.Generation)
		}
	}
	for len(state.IntentTransactions) >= int(MaxAPIntentTransactionsPerUE) {
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
	return request
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
	return result
}
