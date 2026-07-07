package nas_test

import (
	"bytes"
	"testing"

	acoreNas "github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestPlainNasDecodeULCooperationTLVs(t *testing.T) {
	pdu := []byte{
		nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeULCooperation, 0x01,
		0x10, 0x01, 0x01,
		0x18, 0x01, 0x01,
		0x71, 0x02, 0xaa, 0xbb,
	}

	msg := acoreNas.NewMessage()
	if err := msg.PlainNasDecode(&pdu); err != nil {
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
	assertIE(t, ul.GetIE(0x71), 0x71, []byte{0xaa, 0xbb})
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
