package nas_test

import (
	"bytes"
	"testing"

	acoreNas "github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestPlainNasDecodeULCooperationTLVs(t *testing.T) {
	apContents, err := (&nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     nasMessage.APContainerFlagDF,
		Payload:            []byte{0xaa, 0xbb},
	}).Encode()
	if err != nil {
		t.Fatalf("APContainer.Encode() error = %v", err)
	}
	pdu := []byte{
		nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeULCooperation, 0x01,
		0x10, 0x01, 0x01,
		0x18, 0x01, 0x01,
		0x71, byte(len(apContents)),
	}
	pdu = append(pdu, apContents...)

	msg := acoreNas.NewMessage()
	if err = msg.PlainNasDecode(&pdu); err != nil {
		t.Fatalf("PlainNasDecode() error = %v", err)
	}
	if msg.GmmMessage == nil || msg.GmmMessage.ULCooperation == nil {
		t.Fatalf("ULCooperation was not decoded: %#v", msg.GmmMessage)
	}

	ul := msg.GmmMessage.ULCooperation
	if ul.MessageType != acoreNas.MsgTypeULCooperation {
		t.Fatalf("message type = 0x%02x, want 0x%02x", ul.MessageType, acoreNas.MsgTypeULCooperation)
	}
	if ul.MessageIdentity != 0x01 {
		t.Fatalf("message identity = 0x%02x, want 0x01", ul.MessageIdentity)
	}
	if len(ul.IEs) != 3 {
		t.Fatalf("IE count = %d, want 3", len(ul.IEs))
	}

	assertIE(t, ul.GetIE(0x10), 0x10, []byte{0x01})
	assertIE(t, ul.GetIE(0x18), 0x18, []byte{0x01})
	assertIE(t, ul.GetIE(0x71), 0x71, apContents)
	ap, err := nasMessage.DecodeAPContainer(ul.GetIE(0x71).GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if !bytes.Equal(ap.Payload, []byte{0xaa, 0xbb}) {
		t.Fatalf("AP payload = %x, want aabb", ap.Payload)
	}
}

func TestULCooperationOneByteLengthSupports245ByteAPFragment(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5a}, nasMessage.APContainerMaxDLFragmentSize)
	apContents, err := (&nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		ContainerFlags:     nasMessage.APContainerFlagMF,
		Payload:            payload,
	}).Encode()
	if err != nil {
		t.Fatalf("APContainer.Encode() error = %v", err)
	}
	if len(apContents) != 255 {
		t.Fatalf("AP Container length = %d, want 255", len(apContents))
	}

	ul := nasMessage.NewULCooperation(acoreNas.MsgTypeULCooperation)
	ul.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
	if err = ul.AddIE(0x71, apContents); err != nil {
		t.Fatalf("AddIE(0x71) error = %v", err)
	}
	buffer := bytes.NewBuffer(nil)
	if err = ul.EncodeULCooperation(buffer); err != nil {
		t.Fatalf("EncodeULCooperation() error = %v", err)
	}
	encoded := buffer.Bytes()
	if encoded[5] != 0xff {
		t.Fatalf("AP IE length = %d, want 255", encoded[5])
	}

	msg := acoreNas.NewMessage()
	if err = msg.PlainNasDecode(&encoded); err != nil {
		t.Fatalf("PlainNasDecode() error = %v", err)
	}
	decoded, err := nasMessage.DecodeAPContainer(msg.GmmMessage.ULCooperation.GetIE(0x71).GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatal("decoded AP payload does not match 245-byte fragment")
	}
}

func TestDecodeULCooperationRejectsMalformedTLV(t *testing.T) {
	tests := []struct {
		name string
		pdu  []byte
	}{
		{
			name: "missing length",
			pdu: []byte{
				nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeULCooperation, 0x01,
				0x10,
			},
		},
		{
			name: "length exceeds remaining bytes",
			pdu: []byte{
				nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeULCooperation, 0x01,
				0x10, 0x02, 0x01,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := nasMessage.NewULCooperation(acoreNas.MsgTypeULCooperation)
			if err := ul.DecodeULCooperation(&tt.pdu); err == nil {
				t.Fatal("DecodeULCooperation() error = nil, want malformed TLV error")
			}
		})
	}
}

func assertIE(t *testing.T, ie *nasMessage.CooperationIE, wantIEI uint8, wantContents []byte) {
	t.Helper()
	if ie == nil {
		t.Fatalf("IE 0x%02x is nil", wantIEI)
	}
	if ie.GetIei() != wantIEI {
		t.Fatalf("IEI = 0x%02x, want 0x%02x", ie.GetIei(), wantIEI)
	}
	if ie.GetLen() != uint8(len(wantContents)) {
		t.Fatalf("IE 0x%02x len = %d, want %d", wantIEI, ie.GetLen(), len(wantContents))
	}
	if got := ie.GetContents(); !bytes.Equal(got, wantContents) {
		t.Fatalf("IE 0x%02x contents = %x, want %x", wantIEI, got, wantContents)
	}
}
