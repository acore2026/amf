package gmm

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

func AddULAPContainerFragment(
	ue *context.AmfUe,
	accessType models.AccessType,
	messageIdentity uint8,
	fragment *nasMessage.APContainer,
	now time.Time,
) (*nasMessage.APContainer, error) {
	complete, _, err := addULAPContainerFragment(
		ue, accessType, messageIdentity, fragment, nil, now,
	)
	return complete, err
}

func addULAPContainerFragmentWithPendingDL(
	ue *context.AmfUe,
	accessType models.AccessType,
	messageIdentity uint8,
	fragment *nasMessage.APContainer,
	pendingDLIEs []context.DLCooperationIE,
	now time.Time,
) (*nasMessage.APContainer, []context.DLCooperationIE, error) {
	return addULAPContainerFragment(
		ue, accessType, messageIdentity, fragment, pendingDLIEs, now,
	)
}

func addULAPContainerFragment(
	ue *context.AmfUe,
	accessType models.AccessType,
	messageIdentity uint8,
	fragment *nasMessage.APContainer,
	pendingDLIEs []context.DLCooperationIE,
	now time.Time,
) (*nasMessage.APContainer, []context.DLCooperationIE, error) {
	if ue == nil || fragment == nil {
		return nil, nil, fmt.Errorf("UE and AP Container fragment are required")
	}
	cooperation := ue.GetOrCreateCooperationContext()
	apState := cooperation.APContainer
	key := context.APContainerReassemblyKey{
		Direction: context.APContainerDirectionUL,
		PayloadID: fragment.ContainerPayloadID,
	}

	apState.Mu.Lock()
	defer apState.Mu.Unlock()

	state := apState.Reassemblies[key]
	if state == nil {
		if countAPContainerReassemblies(
			apState.Reassemblies, context.APContainerDirectionUL,
		) >= context.MaxAPContainerReassembliesPerDirection {
			return nil, nil, fmt.Errorf("UL AP Container reassembly limit reached")
		}
		apState.NextGeneration++
		state = &context.APContainerReassemblyState{
			ContainerType:    fragment.ContainerType,
			ContainerTypePTI: fragment.ContainerTypePTI,
			DontFragment:     fragment.DontFragment(),
			MessageIdentity:  messageIdentity,
			AccessType:       accessType,
			Fragments:        make(map[uint16]context.APContainerFragment),
			CreatedAt:        now,
			Generation:       apState.NextGeneration,
		}
		apState.Reassemblies[key] = state
		generation := state.Generation
		state.Timer = time.AfterFunc(context.APContainerReassemblyTimeout, func() {
			expireULAPContainerReassembly(ue, key, generation, time.Now())
		})
	}
	if err := validateAPContainerMetadata(state, fragment, accessType, messageIdentity); err != nil {
		clearAPContainerReassemblyLocked(apState, key)
		return nil, nil, err
	}

	incoming := context.APContainerFragment{
		Offset:        fragment.FragmentOffset,
		MoreFragments: fragment.MoreFragments(),
		Payload:       append([]byte(nil), fragment.Payload...),
	}
	if existing, ok := state.Fragments[incoming.Offset]; ok && existing.Equal(incoming) {
		return completeAPContainerLocked(apState, key, state)
	}
	if state.DistinctFragments >= context.MaxAPContainerFragmentsPerPayload {
		clearAPContainerReassemblyLocked(apState, key)
		return nil, nil, fmt.Errorf("AP Container payload 0x%04x exceeds fragment limit",
			fragment.ContainerPayloadID)
	}

	newStart := int(incoming.Offset)
	newEnd := newStart + len(incoming.Payload)
	for _, existing := range state.Fragments {
		oldStart := int(existing.Offset)
		oldEnd := oldStart + len(existing.Payload)
		if newStart < oldEnd && oldStart < newEnd {
			clearAPContainerReassemblyLocked(apState, key)
			return nil, nil, fmt.Errorf("AP Container fragment range [%d,%d) overlaps [%d,%d)",
				newStart, newEnd, oldStart, oldEnd)
		}
	}

	if state.FinalLength != nil && uint32(newEnd) > *state.FinalLength {
		clearAPContainerReassemblyLocked(apState, key)
		return nil, nil, fmt.Errorf("AP Container fragment end %d exceeds final length %d",
			newEnd, *state.FinalLength)
	}
	if !incoming.MoreFragments {
		finalLength := uint32(newEnd)
		if state.FinalLength != nil && *state.FinalLength != finalLength {
			clearAPContainerReassemblyLocked(apState, key)
			return nil, nil, fmt.Errorf("AP Container final length conflict: %d and %d",
				*state.FinalLength, finalLength)
		}
		for _, existing := range state.Fragments {
			existingEnd := uint32(existing.Offset) + uint32(len(existing.Payload))
			if existingEnd > finalLength {
				clearAPContainerReassemblyLocked(apState, key)
				return nil, nil, fmt.Errorf("AP Container existing fragment end %d exceeds final length %d",
					existingEnd, finalLength)
			}
		}
		state.FinalLength = &finalLength
	}

	state.Fragments[incoming.Offset] = incoming
	state.DistinctFragments++
	mergeReassemblyPendingDLIEs(state, pendingDLIEs)
	return completeAPContainerLocked(apState, key, state)
}

func validateAPContainerMetadata(
	state *context.APContainerReassemblyState,
	fragment *nasMessage.APContainer,
	accessType models.AccessType,
	messageIdentity uint8,
) error {
	if state.ContainerType != fragment.ContainerType ||
		state.ContainerTypePTI != fragment.ContainerTypePTI ||
		state.DontFragment != fragment.DontFragment() ||
		state.MessageIdentity != messageIdentity ||
		state.AccessType != accessType {
		return fmt.Errorf("AP Container fragment metadata mismatch for payload 0x%04x",
			fragment.ContainerPayloadID)
	}
	return nil
}

func countAPContainerReassemblies(
	states map[context.APContainerReassemblyKey]*context.APContainerReassemblyState,
	direction context.APContainerDirection,
) int {
	count := 0
	for key := range states {
		if key.Direction == direction {
			count++
		}
	}
	return count
}

func completeAPContainerLocked(
	apState *context.APContainerState,
	key context.APContainerReassemblyKey,
	state *context.APContainerReassemblyState,
) (*nasMessage.APContainer, []context.DLCooperationIE, error) {
	if state.FinalLength == nil {
		return nil, nil, nil
	}
	offsets := make([]int, 0, len(state.Fragments))
	for offset := range state.Fragments {
		offsets = append(offsets, int(offset))
	}
	sort.Ints(offsets)

	payload := make([]byte, 0, int(*state.FinalLength))
	expected := 0
	for _, offset := range offsets {
		fragment := state.Fragments[uint16(offset)]
		if offset != expected {
			return nil, nil, nil
		}
		payload = append(payload, fragment.Payload...)
		expected += len(fragment.Payload)
	}
	if uint32(expected) != *state.FinalLength {
		return nil, nil, nil
	}

	complete := &nasMessage.APContainer{
		ContainerType:      state.ContainerType,
		ContainerTypePTI:   state.ContainerTypePTI,
		ContainerPayloadID: key.PayloadID,
		FragmentOffset:     0,
		Payload:            payload,
	}
	if state.DontFragment {
		complete.ContainerFlags = nasMessage.APContainerFlagDF
	}
	pendingDLIEs := cloneContextDLCooperationIEs(state.PendingDLIEs)
	clearAPContainerReassemblyLocked(apState, key)
	return complete, pendingDLIEs, nil
}

func mergeReassemblyPendingDLIEs(
	state *context.APContainerReassemblyState,
	incoming []context.DLCooperationIE,
) {
	for _, candidate := range incoming {
		replaced := false
		for index := range state.PendingDLIEs {
			if state.PendingDLIEs[index].IEI != candidate.IEI {
				continue
			}
			state.PendingDLIEs[index].Contents = append([]byte(nil), candidate.Contents...)
			replaced = true
			break
		}
		if !replaced {
			state.PendingDLIEs = append(state.PendingDLIEs, context.DLCooperationIE{
				IEI:      candidate.IEI,
				Contents: append([]byte(nil), candidate.Contents...),
			})
		}
	}
}

func cloneContextDLCooperationIEs(
	ies []context.DLCooperationIE,
) []context.DLCooperationIE {
	if len(ies) == 0 {
		return nil
	}
	result := make([]context.DLCooperationIE, len(ies))
	for index, ie := range ies {
		result[index] = context.DLCooperationIE{
			IEI:      ie.IEI,
			Contents: append([]byte(nil), ie.Contents...),
		}
	}
	return result
}

func clearAPContainerReassemblyLocked(
	state *context.APContainerState,
	key context.APContainerReassemblyKey,
) {
	if current := state.Reassemblies[key]; current != nil && current.Timer != nil {
		current.Timer.Stop()
	}
	delete(state.Reassemblies, key)
}

func expireULAPContainerReassembly(
	ue *context.AmfUe,
	key context.APContainerReassemblyKey,
	generation uint64,
	now time.Time,
) bool {
	cooperation := ue.CooperationContext
	if cooperation == nil || cooperation.APContainer == nil {
		return false
	}
	apState := cooperation.APContainer
	apState.Mu.Lock()
	current := apState.Reassemblies[key]
	if current == nil || current.Generation != generation {
		apState.Mu.Unlock()
		return false
	}
	if now.Before(current.CreatedAt.Add(context.APContainerReassemblyTimeout)) {
		apState.Mu.Unlock()
		return false
	}
	missing := describeMissingAPContainerRanges(current)
	fragmentCount := current.DistinctFragments
	delete(apState.Reassemblies, key)
	apState.Mu.Unlock()

	if ue.GmmLog != nil {
		ue.GmmLog.Warnf(
			"UL AP Container payload 0x%04x timed out after %d fragments; missing=%s",
			key.PayloadID, fragmentCount, missing,
		)
	}
	return true
}

func describeMissingAPContainerRanges(state *context.APContainerReassemblyState) string {
	if state.FinalLength == nil {
		return "final-length-unknown"
	}
	offsets := make([]int, 0, len(state.Fragments))
	for offset := range state.Fragments {
		offsets = append(offsets, int(offset))
	}
	sort.Ints(offsets)

	cursor := 0
	missing := make([]string, 0)
	for _, offset := range offsets {
		fragment := state.Fragments[uint16(offset)]
		if offset > cursor {
			missing = append(missing, fmt.Sprintf("[%d,%d)", cursor, offset))
		}
		end := offset + len(fragment.Payload)
		if end > cursor {
			cursor = end
		}
	}
	if cursor < int(*state.FinalLength) {
		missing = append(missing, fmt.Sprintf("[%d,%d)", cursor, *state.FinalLength))
	}
	if len(missing) == 0 {
		return "none"
	}
	return strings.Join(missing, ",")
}
