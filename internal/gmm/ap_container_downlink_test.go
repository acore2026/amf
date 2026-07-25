package gmm

import (
	"bytes"
	"testing"

	"github.com/acore2026/nas/nasMessage"
)

func TestBuildDLAPContainerIEsFragmentsAt245Bytes(t *testing.T) {
	payload := make([]byte, 500)
	for i := range payload {
		payload[i] = byte(i)
	}
	complete := &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		Payload:            payload,
	}

	ies, err := buildDLAPContainerIEs(0x01, complete)
	if err != nil {
		t.Fatalf("buildDLAPContainerIEs() error = %v", err)
	}
	if len(ies) != 3 {
		t.Fatalf("IE count = %d, want 3", len(ies))
	}
	wantOffsets := []uint16{0, 245, 490}
	wantLengths := []int{245, 245, 10}
	reassembled := make([]byte, 0, len(payload))
	for i, ie := range ies {
		fragment, err := nasMessage.DecodeAPContainer(ie.GetContents())
		if err != nil {
			t.Fatalf("fragment %d decode error = %v", i, err)
		}
		if fragment.FragmentOffset != wantOffsets[i] || len(fragment.Payload) != wantLengths[i] {
			t.Fatalf("fragment %d offset=%d length=%d", i, fragment.FragmentOffset, len(fragment.Payload))
		}
		if fragment.MoreFragments() != (i < 2) || fragment.DontFragment() {
			t.Fatalf("fragment %d DF=%v MF=%v", i, fragment.DontFragment(), fragment.MoreFragments())
		}
		reassembled = append(reassembled, fragment.Payload...)
	}
	if !bytes.Equal(reassembled, payload) {
		t.Fatal("DL fragments do not reconstruct the complete payload")
	}
}

func TestBuildDLAPContainerIEsUsesLargeLegacyFragments(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5a}, 1500)
	complete := &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		Payload:            payload,
	}

	ies, err := buildDLAPContainerIEs(0x02, complete)
	if err != nil {
		t.Fatalf("buildDLAPContainerIEs() error = %v", err)
	}
	if len(ies) != 2 {
		t.Fatalf("IE count = %d, want 2", len(ies))
	}
	wantOffsets := []uint16{0, 1400}
	wantLengths := []int{1400, 100}
	for i, ie := range ies {
		if ie.LegacyLen == 0 {
			t.Fatalf("fragment %d LegacyLen = 0, want legacy length", i)
		}
		fragment, err := nasMessage.DecodeAPContainer(ie.GetContents())
		if err != nil {
			t.Fatalf("fragment %d decode error = %v", i, err)
		}
		if fragment.FragmentOffset != wantOffsets[i] || len(fragment.Payload) != wantLengths[i] {
			t.Fatalf("fragment %d offset=%d length=%d", i, fragment.FragmentOffset, len(fragment.Payload))
		}
		if fragment.MoreFragments() != (i == 0) {
			t.Fatalf("fragment %d MF=%v", i, fragment.MoreFragments())
		}
	}
}

func TestBuildDLAPContainerIEsKeepsDFInOneMessage(t *testing.T) {
	complete := &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     nasMessage.APContainerFlagDF,
		Payload:            bytes.Repeat([]byte{0xaa}, 245),
	}
	ies, err := buildDLAPContainerIEs(0x01, complete)
	if err != nil {
		t.Fatalf("buildDLAPContainerIEs() error = %v", err)
	}
	if len(ies) != 1 {
		t.Fatalf("IE count = %d, want 1", len(ies))
	}
	fragment, err := nasMessage.DecodeAPContainer(ies[0].GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if !fragment.DontFragment() || fragment.MoreFragments() || fragment.FragmentOffset != 0 {
		t.Fatalf("DF fragment = %#v", fragment)
	}
}

func TestBuildDLAPContainerIEsRejectsOversizedOneByteDFPayload(t *testing.T) {
	complete := &nasMessage.APContainer{
		ContainerFlags: nasMessage.APContainerFlagDF,
		Payload:        bytes.Repeat([]byte{0xaa}, 246),
	}
	if _, err := buildDLAPContainerIEs(0x01, complete); err == nil {
		t.Fatal("buildDLAPContainerIEs() error = nil")
	}
}

func TestBuildDLAPContainerIEsAllowsLargeLegacyDFPayload(t *testing.T) {
	complete := &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     nasMessage.APContainerFlagDF,
		Payload:            bytes.Repeat([]byte{0xaa}, 300),
	}
	ies, err := buildDLAPContainerIEs(0x02, complete)
	if err != nil {
		t.Fatalf("buildDLAPContainerIEs() error = %v", err)
	}
	if len(ies) != 1 {
		t.Fatalf("IE count = %d, want 1", len(ies))
	}
	if ies[0].LegacyLen != uint16(nasMessage.APContainerHeaderLength+len(complete.Payload)) {
		t.Fatalf("LegacyLen = %d", ies[0].LegacyLen)
	}
}

func TestBuildDLAPContainerIEsAllowsEmptyPayload(t *testing.T) {
	complete := &nasMessage.APContainer{ContainerPayloadID: 1}
	ies, err := buildDLAPContainerIEs(0x01, complete)
	if err != nil {
		t.Fatalf("buildDLAPContainerIEs() error = %v", err)
	}
	if len(ies) != 1 {
		t.Fatalf("IE count = %d, want 1", len(ies))
	}
	fragment, err := nasMessage.DecodeAPContainer(ies[0].GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if len(fragment.Payload) != 0 || fragment.FragmentOffset != 0 || fragment.MoreFragments() {
		t.Fatalf("empty fragment = %#v", fragment)
	}
}
