package context

import (
	"bytes"
	"sync"
	"time"

	"github.com/acore2026/openapi/models"
)

const (
	MaxAPContainerReassembliesPerDirection = 8
	MaxAPContainerFragmentsPerPayload      = 1024
	MaxCompletedAPContainers               = 8
	APContainerReassemblyTimeout           = 30 * time.Second
)

type APContainerDirection uint8

const (
	APContainerDirectionUL APContainerDirection = iota + 1
	APContainerDirectionDL
)

type APContainerReassemblyKey struct {
	Direction APContainerDirection
	PayloadID uint16
}

type APContainerFragment struct {
	Offset        uint16
	MoreFragments bool
	Payload       []byte
}

func (f APContainerFragment) Equal(other APContainerFragment) bool {
	return f.Offset == other.Offset &&
		f.MoreFragments == other.MoreFragments &&
		bytes.Equal(f.Payload, other.Payload)
}

type APContainerReassemblyState struct {
	ContainerType     uint16
	ContainerTypePTI  uint8
	DontFragment      bool
	MessageIdentity   uint8
	AccessType        models.AccessType
	Fragments         map[uint16]APContainerFragment
	FinalLength       *uint32
	DistinctFragments int
	CreatedAt         time.Time
	Generation        uint64
	Timer             *time.Timer
}

type CompletedAPContainer struct {
	ContainerType      uint16
	ContainerTypePTI   uint8
	ContainerPayloadID uint16
	Payload            []byte
	CompletedAt        time.Time
}

type APContainerState struct {
	Mu                   sync.Mutex
	Reassemblies         map[APContainerReassemblyKey]*APContainerReassemblyState
	Completed            map[uint16]CompletedAPContainer
	IntentTransactions   map[uint16]*APIntentTransaction
	NextGeneration       uint64
	NextIntentGeneration uint64
}

func NewCooperationContext() *CooperationContext {
	return &CooperationContext{
		LastULIEs:     make(map[uint8][][]byte),
		NegotiatedIEs: make(map[uint8][]byte),
		APContainer: &APContainerState{
			Reassemblies:       make(map[APContainerReassemblyKey]*APContainerReassemblyState),
			Completed:          make(map[uint16]CompletedAPContainer),
			IntentTransactions: make(map[uint16]*APIntentTransaction),
		},
	}
}

func (ue *AmfUe) GetOrCreateCooperationContext() *CooperationContext {
	ue.Lock.Lock()
	defer ue.Lock.Unlock()

	if ue.CooperationContext == nil {
		ue.CooperationContext = NewCooperationContext()
		return ue.CooperationContext
	}
	if ue.CooperationContext.LastULIEs == nil {
		ue.CooperationContext.LastULIEs = make(map[uint8][][]byte)
	}
	if ue.CooperationContext.NegotiatedIEs == nil {
		ue.CooperationContext.NegotiatedIEs = make(map[uint8][]byte)
	}
	if ue.CooperationContext.APContainer == nil {
		ue.CooperationContext.APContainer = &APContainerState{
			Reassemblies:       make(map[APContainerReassemblyKey]*APContainerReassemblyState),
			Completed:          make(map[uint16]CompletedAPContainer),
			IntentTransactions: make(map[uint16]*APIntentTransaction),
		}
	} else {
		if ue.CooperationContext.APContainer.Reassemblies == nil {
			ue.CooperationContext.APContainer.Reassemblies =
				make(map[APContainerReassemblyKey]*APContainerReassemblyState)
		}
		if ue.CooperationContext.APContainer.Completed == nil {
			ue.CooperationContext.APContainer.Completed = make(map[uint16]CompletedAPContainer)
		}
		if ue.CooperationContext.APContainer.IntentTransactions == nil {
			ue.CooperationContext.APContainer.IntentTransactions = make(map[uint16]*APIntentTransaction)
		}
	}
	return ue.CooperationContext
}

func (c *CooperationContext) StoreCompletedAPContainer(record CompletedAPContainer) {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()

	record.Payload = append([]byte(nil), record.Payload...)
	if _, exists := state.Completed[record.ContainerPayloadID]; !exists &&
		len(state.Completed) >= MaxCompletedAPContainers {
		var oldestID uint16
		var oldestTime time.Time
		first := true
		for id, existing := range state.Completed {
			if first || existing.CompletedAt.Before(oldestTime) {
				first = false
				oldestID = id
				oldestTime = existing.CompletedAt
			}
		}
		delete(state.Completed, oldestID)
	}
	state.Completed[record.ContainerPayloadID] = record
}

func (c *CooperationContext) CompletedAPContainers() map[uint16]CompletedAPContainer {
	state := c.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()

	result := make(map[uint16]CompletedAPContainer, len(state.Completed))
	for id, record := range state.Completed {
		record.Payload = append([]byte(nil), record.Payload...)
		result[id] = record
	}
	return result
}

func (ue *AmfUe) StopAPContainerReassemblyTimers() {
	ue.Lock.Lock()
	ctx := ue.CooperationContext
	ue.Lock.Unlock()
	if ctx == nil || ctx.APContainer == nil {
		return
	}
	state := ctx.APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()

	for key, reassembly := range state.Reassemblies {
		if reassembly.Timer != nil {
			reassembly.Timer.Stop()
		}
		delete(state.Reassemblies, key)
	}
	for payloadID, transaction := range state.IntentTransactions {
		if transaction.Cancel != nil {
			transaction.Cancel()
		}
		if transaction.Timer != nil {
			transaction.Timer.Stop()
		}
		delete(state.IntentTransactions, payloadID)
	}
}
