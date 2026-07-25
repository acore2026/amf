package nas_test

import (
	"bytes"
	"testing"

	acoreNas "github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestEncodeDLCooperationTLVs(t *testing.T) {
	apContents := mustEncodeTestAPContainer(t, []byte{0xaa, 0xbb}, nasMessage.APContainerFlagDF)
	tests := []struct {
		name string
		ies  map[uint8][]byte
		want []byte
	}{
		{
			name: "IE 0x10 disabled",
			ies:  map[uint8][]byte{0x10: {0x00}},
			want: []byte{nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeDLCooperation, 0x01, 0x10, 0x01, 0x00},
		},
		{
			name: "IE 0x10 enabled",
			ies:  map[uint8][]byte{0x10: {0x01}},
			want: []byte{nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeDLCooperation, 0x01, 0x10, 0x01, 0x01},
		},
		{
			name: "IE 0x71",
			ies:  map[uint8][]byte{0x71: apContents},
			want: append([]byte{nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeDLCooperation, 0x01, 0x71, byte(len(apContents))}, apContents...),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dl := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
			dl.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
			dl.SpareHalfOctetAndSecurityHeaderType.SetSecurityHeaderType(0)
			dl.MessageIdentity = 0x01
			for iei, contents := range tt.ies {
				if err := dl.AddIE(iei, contents); err != nil {
					t.Fatalf("AddIE() error = %v", err)
				}
			}

			buffer := bytes.NewBuffer(nil)
			if err := dl.EncodeDLCooperation(buffer); err != nil {
				t.Fatalf("EncodeDLCooperation() error = %v", err)
			}
			if got := buffer.Bytes(); !bytes.Equal(got, tt.want) {
				t.Fatalf("encoded DLCooperation = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestEncodeDLCooperationWithMultipleTLVs(t *testing.T) {
	apContents := mustEncodeTestAPContainer(t, []byte{0xaa, 0xbb}, nasMessage.APContainerFlagDF)
	dl := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
	dl.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
	dl.SpareHalfOctetAndSecurityHeaderType.SetSecurityHeaderType(0)
	dl.MessageIdentity = 0x01
	if err := dl.AddIE(0x10, []byte{0x01}); err != nil {
		t.Fatalf("AddIE(0x10) error = %v", err)
	}
	if err := dl.AddIE(0x71, apContents); err != nil {
		t.Fatalf("AddIE(0x71) error = %v", err)
	}

	buffer := bytes.NewBuffer(nil)
	if err := dl.EncodeDLCooperation(buffer); err != nil {
		t.Fatalf("EncodeDLCooperation() error = %v", err)
	}

	want := []byte{nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeDLCooperation, 0x01, 0x10, 0x01, 0x01, 0x71, byte(len(apContents))}
	want = append(want, apContents...)
	if got := buffer.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("encoded DLCooperation = %x, want %x", got, want)
	}
}

func TestDLCooperationOneByteLengthSupports245ByteAPFragment(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5a}, nasMessage.APContainerMaxOneByteDLFragmentSize)
	apContents := mustEncodeTestAPContainer(t, payload, nasMessage.APContainerFlagMF)
	if len(apContents) != 255 {
		t.Fatalf("AP Container length = %d, want 255", len(apContents))
	}

	dl := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
	dl.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
	if err := dl.AddIE(0x71, apContents); err != nil {
		t.Fatalf("AddIE(0x71) error = %v", err)
	}
	buffer := bytes.NewBuffer(nil)
	if err := dl.EncodeDLCooperation(buffer); err != nil {
		t.Fatalf("EncodeDLCooperation() error = %v", err)
	}
	encoded := buffer.Bytes()
	if encoded[5] != 0xff {
		t.Fatalf("AP IE length = %d, want 255", encoded[5])
	}

	decoded := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
	if err := decoded.DecodeDLCooperation(&encoded); err != nil {
		t.Fatalf("DecodeDLCooperation() error = %v", err)
	}
	ap, err := nasMessage.DecodeAPContainer(decoded.GetIE(0x71).GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if !bytes.Equal(ap.Payload, payload) {
		t.Fatal("decoded AP payload does not match 245-byte fragment")
	}
}

func TestDLCooperationLegacyOuterLengthCarriesNewAPHeader(t *testing.T) {
	apContents := mustEncodeTestAPContainer(t, []byte{0xaa, 0xbb}, nasMessage.APContainerFlagDF)
	dl := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
	dl.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
	dl.MessageIdentity = 0x02
	if err := dl.AddIE(0x71, apContents); err != nil {
		t.Fatalf("AddIE(0x71) error = %v", err)
	}
	buffer := bytes.NewBuffer(nil)
	if err := dl.EncodeDLCooperation(buffer); err != nil {
		t.Fatalf("EncodeDLCooperation() error = %v", err)
	}
	encoded := buffer.Bytes()
	if got, want := encoded[4:7], []byte{0x71, 0x00, byte(len(apContents))}; !bytes.Equal(got, want) {
		t.Fatalf("legacy AP IE header = %x, want %x", got, want)
	}

	decoded := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
	if err := decoded.DecodeDLCooperation(&encoded); err != nil {
		t.Fatalf("DecodeDLCooperation() error = %v", err)
	}
	ap, err := nasMessage.DecodeAPContainer(decoded.GetIE(0x71).GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if !bytes.Equal(ap.Payload, []byte{0xaa, 0xbb}) {
		t.Fatalf("AP payload = %x, want aabb", ap.Payload)
	}
}

func mustEncodeTestAPContainer(t *testing.T, payload []byte, flags uint8) []byte {
	t.Helper()
	contents, err := (&nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     flags,
		Payload:            payload,
	}).Encode()
	if err != nil {
		t.Fatalf("APContainer.Encode() error = %v", err)
	}
	return contents
}
