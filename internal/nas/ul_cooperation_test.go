package nas_test

import (
	"bytes"
	"testing"

	acoreNas "github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestPlainNasDecodeULCooperation(t *testing.T) {
	pdu := []byte{
		nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeULCooperation,
		nasMessage.ULCooperationUeCapType, 0x00, 0x02, 0xaa, 0xbb,
		nasMessage.ULCooperationOsTypeType, 0x00, 0x01, 0x01,
		nasMessage.ULCooperationULApContainerType, 0x00, 0x03, 0x10, 0x20, 0x30,
		nasMessage.ULCooperationCooperInfoContainerType, 0x00, 0x02, 0xcc, 0xdd,
	}

	msg := acoreNas.NewMessage()
	if err := msg.PlainNasDecode(&pdu); err != nil {
		t.Fatalf("PlainNasDecode() error = %v", err)
	}
	if msg.GmmMessage == nil || msg.GmmMessage.ULCooperation == nil {
		t.Fatalf("ULCooperation was not decoded: %#v", msg.GmmMessage)
	}
	ul := msg.GmmMessage.ULCooperation
	if ul.GetMessageType() != acoreNas.MsgTypeULCooperation {
		t.Fatalf("message type = 0x%02x, want 0x%02x", ul.GetMessageType(), acoreNas.MsgTypeULCooperation)
	}
	if got := ul.UeCap.GetContents(); !bytes.Equal(got, []byte{0xaa, 0xbb}) {
		t.Fatalf("UeCap = %x", got)
	}
	if got := ul.OsType.GetContents(); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("OsType = %x", got)
	}
}
