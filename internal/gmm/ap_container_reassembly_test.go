package gmm

import (
	"bytes"
	"testing"
	"time"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

func apFragment(id, offset uint16, mf bool, payload []byte) *nasMessage.APContainer {
	flags := uint8(0)
	if mf {
		flags |= nasMessage.APContainerFlagMF
	}
	return &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: id,
		ContainerFlags:     flags,
		FragmentOffset:     offset,
		Payload:            append([]byte(nil), payload...),
	}
}

func TestAddULAPContainerFragmentReassemblesOutOfOrder(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	second := apFragment(0x12, 3, false, []byte("def"))
	first := apFragment(0x12, 0, true, []byte("abc"))

	complete, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01, second, now,
	)
	if err != nil || complete != nil {
		t.Fatalf("first result complete=%#v err=%v", complete, err)
	}
	complete, err = AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01, first, now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("second fragment error = %v", err)
	}
	if complete == nil || !bytes.Equal(complete.Payload, []byte("abcdef")) {
		t.Fatalf("complete = %#v", complete)
	}
	if complete.ContainerPayloadID != 0x12 || complete.FragmentOffset != 0 || complete.MoreFragments() {
		t.Fatalf("completed metadata = %#v", complete)
	}
}

func TestAddULAPContainerFragmentIgnoresExactDuplicate(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	first := apFragment(1, 0, true, []byte("abc"))

	if complete, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01, first, now,
	); err != nil || complete != nil {
		t.Fatalf("first result complete=%#v err=%v", complete, err)
	}
	if complete, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01, first, now.Add(time.Second),
	); err != nil || complete != nil {
		t.Fatalf("duplicate result complete=%#v err=%v", complete, err)
	}

	state := ue.GetOrCreateCooperationContext().APContainer
	state.Mu.Lock()
	gotCount := state.Reassemblies[context.APContainerReassemblyKey{
		Direction: context.APContainerDirectionUL,
		PayloadID: 1,
	}].DistinctFragments
	state.Mu.Unlock()
	if gotCount != 1 {
		t.Fatalf("distinct fragments = %d, want 1", gotCount)
	}
}

func TestAddULAPContainerFragmentRejectsOverlap(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	_, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, 0, true, []byte("abcd")), now,
	)
	if err != nil {
		t.Fatalf("first fragment error = %v", err)
	}
	_, err = AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, 2, false, []byte("cdef")), now.Add(time.Second),
	)
	if err == nil {
		t.Fatal("overlap error = nil")
	}
	assertNoAPReassemblies(t, ue)
}

func TestAddULAPContainerFragmentRejectsMetadataMismatch(t *testing.T) {
	tests := []struct {
		name            string
		mutate          func(*nasMessage.APContainer)
		accessType      models.AccessType
		messageIdentity uint8
	}{
		{"container type", func(f *nasMessage.APContainer) { f.ContainerType++ }, models.AccessType__3_GPP_ACCESS, 0x01},
		{"PTI", func(f *nasMessage.APContainer) { f.ContainerTypePTI++ }, models.AccessType__3_GPP_ACCESS, 0x01},
		{"DF", func(f *nasMessage.APContainer) { f.ContainerFlags |= nasMessage.APContainerFlagDF }, models.AccessType__3_GPP_ACCESS, 0x01},
		{"message identity", func(*nasMessage.APContainer) {}, models.AccessType__3_GPP_ACCESS, 0x02},
		{"access type", func(*nasMessage.APContainer) {}, models.AccessType_NON_3_GPP_ACCESS, 0x01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ue := &context.AmfUe{}
			t.Cleanup(ue.StopAPContainerReassemblyTimers)
			now := time.Unix(1000, 0)
			if _, err := AddULAPContainerFragment(
				ue, models.AccessType__3_GPP_ACCESS, 0x01,
				apFragment(1, 0, true, []byte("abc")), now,
			); err != nil {
				t.Fatalf("first fragment error = %v", err)
			}
			second := apFragment(1, 3, false, []byte("def"))
			tt.mutate(second)
			if _, err := AddULAPContainerFragment(
				ue, tt.accessType, tt.messageIdentity, second, now.Add(time.Second),
			); err == nil {
				t.Fatal("metadata mismatch error = nil")
			}
			assertNoAPReassemblies(t, ue)
		})
	}
}

func TestAddULAPContainerFragmentRejectsFinalLengthConflict(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	if _, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, 6, false, []byte("ghi")), now,
	); err != nil {
		t.Fatalf("first final fragment error = %v", err)
	}
	if _, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, 3, false, []byte("de")), now.Add(time.Second),
	); err == nil {
		t.Fatal("final length conflict error = nil")
	}
	assertNoAPReassemblies(t, ue)
}

func TestHistoricalAPContainerDoesNotCompleteAsNewFormat(t *testing.T) {
	oldContents := []byte{
		0x01, 0x00, 0x00, 0x07, 0x05,
		0x00, 0x00, 0x00, 0x05,
		0x7b, 0x7d,
	}
	fragment, err := nasMessage.DecodeAPContainer(oldContents)
	if err != nil {
		t.Fatalf("historical bytes decode error = %v", err)
	}
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	complete, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x02, fragment, time.Unix(1000, 0),
	)
	if err != nil {
		t.Fatalf("historical bytes reassembly error = %v", err)
	}
	if complete != nil {
		t.Fatalf("old four-byte ContainerContent completed as new format: %#v", complete)
	}
}

func TestAddULAPContainerFragmentLimitsConcurrentPayloads(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	for id := uint16(1); id <= context.MaxAPContainerReassembliesPerDirection; id++ {
		if _, err := AddULAPContainerFragment(
			ue, models.AccessType__3_GPP_ACCESS, 0x01,
			apFragment(id, 0, true, []byte{byte(id)}), now,
		); err != nil {
			t.Fatalf("payload %d error = %v", id, err)
		}
	}
	if _, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(9, 0, true, []byte{0x09}), now,
	); err == nil {
		t.Fatal("ninth payload error = nil")
	}
	if got := apReassemblyCount(ue); got != context.MaxAPContainerReassembliesPerDirection {
		t.Fatalf("reassembly count = %d, want %d", got, context.MaxAPContainerReassembliesPerDirection)
	}
}

func TestAddULAPContainerFragmentLimitsDistinctFragments(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	for offset := 0; offset < context.MaxAPContainerFragmentsPerPayload; offset++ {
		if _, err := AddULAPContainerFragment(
			ue, models.AccessType__3_GPP_ACCESS, 0x01,
			apFragment(1, uint16(offset), true, []byte{byte(offset)}), now,
		); err != nil {
			t.Fatalf("fragment %d error = %v", offset, err)
		}
	}
	if _, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, uint16(context.MaxAPContainerFragmentsPerPayload), true, []byte{0xff}), now,
	); err == nil {
		t.Fatal("fragment limit error = nil")
	}
	assertNoAPReassemblies(t, ue)
}

func TestExpireULAPContainerReassemblyUsesAbsoluteTimeoutAndGeneration(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	now := time.Unix(1000, 0)
	if _, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, 0, true, []byte("abc")), now,
	); err != nil {
		t.Fatalf("first fragment error = %v", err)
	}
	key := context.APContainerReassemblyKey{Direction: context.APContainerDirectionUL, PayloadID: 1}
	firstGeneration := apReassemblyGeneration(t, ue, key)
	if expireULAPContainerReassembly(ue, key, firstGeneration, now.Add(29*time.Second)) {
		t.Fatal("reassembly expired before 30 seconds")
	}
	if !expireULAPContainerReassembly(ue, key, firstGeneration, now.Add(30*time.Second)) {
		t.Fatal("reassembly did not expire at 30 seconds")
	}

	if _, err := AddULAPContainerFragment(
		ue, models.AccessType__3_GPP_ACCESS, 0x01,
		apFragment(1, 0, true, []byte("new")), now.Add(31*time.Second),
	); err != nil {
		t.Fatalf("reused payload ID error = %v", err)
	}
	secondGeneration := apReassemblyGeneration(t, ue, key)
	if secondGeneration == firstGeneration {
		t.Fatal("reused payload ID did not get a new generation")
	}
	if expireULAPContainerReassembly(ue, key, firstGeneration, now.Add(time.Minute)) {
		t.Fatal("stale generation expired the new reassembly")
	}
	if got := apReassemblyCount(ue); got != 1 {
		t.Fatalf("reassembly count = %d, want 1", got)
	}
}

func apReassemblyCount(ue *context.AmfUe) int {
	state := ue.GetOrCreateCooperationContext().APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	return len(state.Reassemblies)
}

func apReassemblyGeneration(
	t *testing.T,
	ue *context.AmfUe,
	key context.APContainerReassemblyKey,
) uint64 {
	t.Helper()
	state := ue.GetOrCreateCooperationContext().APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	reassembly := state.Reassemblies[key]
	if reassembly == nil {
		t.Fatalf("reassembly %#v is nil", key)
	}
	return reassembly.Generation
}

func assertNoAPReassemblies(t *testing.T, ue *context.AmfUe) {
	t.Helper()
	state := ue.GetOrCreateCooperationContext().APContainer
	state.Mu.Lock()
	defer state.Mu.Unlock()
	if len(state.Reassemblies) != 0 {
		t.Fatalf("reassembly count = %d, want 0", len(state.Reassemblies))
	}
}
