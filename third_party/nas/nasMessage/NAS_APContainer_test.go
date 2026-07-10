package nasMessage

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAPContainerEncodeDecode(t *testing.T) {
	input := &APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     APContainerFlagMF,
		FragmentOffset:     0x0100,
		Payload:            []byte{0xaa, 0xbb, 0xcc},
	}

	encoded, err := input.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	want := []byte{
		0x01, 0x00,
		0x00, 0x09,
		0x05,
		0x12, 0x34,
		0x04,
		0x01, 0x00,
		0xaa, 0xbb, 0xcc,
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("Encode() = %x, want %x", encoded, want)
	}

	decoded, err := DecodeAPContainer(encoded)
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if decoded.ContainerType != input.ContainerType ||
		decoded.ContainerTypePTI != input.ContainerTypePTI ||
		decoded.ContainerPayloadID != input.ContainerPayloadID ||
		decoded.ContainerFlags != input.ContainerFlags ||
		decoded.FragmentOffset != input.FragmentOffset ||
		!bytes.Equal(decoded.Payload, input.Payload) {
		t.Fatalf("decoded = %#v, want %#v", decoded, input)
	}
	if decoded.DontFragment() || !decoded.MoreFragments() {
		t.Fatalf("decoded flags DF=%v MF=%v", decoded.DontFragment(), decoded.MoreFragments())
	}
}

func TestDecodeAPContainerRejectsInvalidValues(t *testing.T) {
	valid := []byte{0x01, 0x00, 0x00, 0x07, 0x05, 0x12, 0x34, 0x00, 0x00, 0x00, 0xaa}
	tests := []struct {
		name  string
		value []byte
	}{
		{"short header", valid[:9]},
		{"content length below fixed fields", []byte{0x01, 0x00, 0x00, 0x05, 0x05, 0x12, 0x34, 0x00, 0x00, 0x00}},
		{"content length mismatch", []byte{0x01, 0x00, 0x00, 0x08, 0x05, 0x12, 0x34, 0x00, 0x00, 0x00, 0xaa}},
		{"reserved flag", []byte{0x01, 0x00, 0x00, 0x07, 0x05, 0x12, 0x34, 0x01, 0x00, 0x00, 0xaa}},
		{"DF with MF", []byte{0x01, 0x00, 0x00, 0x07, 0x05, 0x12, 0x34, 0x06, 0x00, 0x00, 0xaa}},
		{"DF with offset", []byte{0x01, 0x00, 0x00, 0x07, 0x05, 0x12, 0x34, 0x02, 0x00, 0x01, 0xaa}},
		{"empty with MF", []byte{0x01, 0x00, 0x00, 0x06, 0x05, 0x12, 0x34, 0x04, 0x00, 0x00}},
		{"empty with offset", []byte{0x01, 0x00, 0x00, 0x06, 0x05, 0x12, 0x34, 0x00, 0x00, 0x01}},
		{"fragment end over 65535", []byte{0x01, 0x00, 0x00, 0x08, 0x05, 0x12, 0x34, 0x00, 0xff, 0xff, 0xaa, 0xbb}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeAPContainer(tt.value); err == nil {
				t.Fatal("DecodeAPContainer() error = nil")
			}
		})
	}
}

func TestAPContainerAllowsEmptyCompletePayload(t *testing.T) {
	encoded, err := (&APContainer{ContainerPayloadID: 1}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if len(encoded) != APContainerHeaderLength {
		t.Fatalf("encoded length = %d, want %d", len(encoded), APContainerHeaderLength)
	}
}

func TestAPContainerEncodeRejectsInvalidFlags(t *testing.T) {
	container := &APContainer{
		ContainerFlags: APContainerFlagDF | APContainerFlagMF,
		Payload:        []byte{0x01},
	}
	if _, err := container.Encode(); err == nil {
		t.Fatal("Encode() error = nil")
	}
}

func TestULCooperationEncodesLegacyAPContainerLength(t *testing.T) {
	container := &APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     APContainerFlagDF,
		Payload:            bytes.Repeat([]byte{0xaa}, 300),
	}
	contents, err := container.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	ul := NewULCooperation(0xe1)
	ul.SetExtendedProtocolDiscriminator(Epd5GSMobilityManagementMessage)
	ul.MessageIdentity = 0x02
	ul.IEs = append(ul.IEs, NewCooperationIELegacy(CooperationIEType71, contents))

	buffer := bytes.NewBuffer(nil)
	if err := ul.EncodeULCooperation(buffer); err != nil {
		t.Fatalf("EncodeULCooperation() error = %v", err)
	}
	encoded := buffer.Bytes()
	if len(encoded) < 7 {
		t.Fatalf("encoded length = %d", len(encoded))
	}
	if encoded[4] != CooperationIEType71 {
		t.Fatalf("IEI = 0x%02x, want 0x71", encoded[4])
	}
	if got := binary.BigEndian.Uint16(encoded[5:7]); got != uint16(len(contents)) {
		t.Fatalf("legacy length = %d, want %d", got, len(contents))
	}
}
