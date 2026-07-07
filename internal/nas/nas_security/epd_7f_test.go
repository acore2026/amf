package nas_security_test

import (
	"bytes"
	"testing"

	"github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestDecodeULCooperationV2WithEPD7fTLVs(t *testing.T) {
	nasPdu := []byte{
		0x7f, 0x00, nas.MsgTypeULCooperation, 0x01,
		0x10, 0x01, 0x01,
		0x18, 0x01, 0x01,
		0x71, 0x02, 0xaa, 0xbb,
	}

	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	if err := ul.DecodeULCooperationV2(&nasPdu); err != nil {
		t.Fatalf("DecodeULCooperationV2() error = %v", err)
	}

	if ul.ExtendedProtocolDiscriminator.Octet != 0x7f {
		t.Fatalf("EPD = 0x%02x, want 0x7f", ul.ExtendedProtocolDiscriminator.Octet)
	}
	if ul.MessageType != nas.MsgTypeULCooperation {
		t.Fatalf("MessageType = 0x%02x, want 0x%02x", ul.MessageType, nas.MsgTypeULCooperation)
	}
	if ul.MessageIdentity != 0x01 {
		t.Fatalf("MessageIdentity = 0x%02x, want 0x01", ul.MessageIdentity)
	}
	if len(ul.IEs) != 3 {
		t.Fatalf("IE count = %d, want 3", len(ul.IEs))
	}
	if got := ul.GetIE(0x10).GetContents(); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("IE 0x10 contents = %x, want 01", got)
	}
	if got := ul.GetIE(0x18).GetContents(); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("IE 0x18 contents = %x, want 01", got)
	}
	if got := ul.GetIE(0x71).GetContents(); !bytes.Equal(got, []byte{0xaa, 0xbb}) {
		t.Fatalf("IE 0x71 contents = %x, want aabb", got)
	}
}
