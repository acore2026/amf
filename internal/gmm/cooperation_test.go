package gmm

import (
	"bytes"
	"testing"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

func TestProcessULCooperationStoresRawAndNegotiatedIEs(t *testing.T) {
	ue := &context.AmfUe{}
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 0x01
	mustAddIE(t, ul, 0x10, []byte{0x01})
	mustAddIE(t, ul, 0x18, []byte{0x01})
	mustAddIE(t, ul, 0x71, []byte{0xaa, 0xbb})

	dlIEs, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}

	if ue.CooperationContext == nil {
		t.Fatal("CooperationContext is nil")
	}
	if ue.CooperationContext.LastMessageIdentity != 0x01 {
		t.Fatalf("LastMessageIdentity = 0x%02x, want 0x01", ue.CooperationContext.LastMessageIdentity)
	}
	assertStoredIE(t, ue.CooperationContext.LastULIEs, 0x10, [][]byte{{0x01}})
	assertStoredIE(t, ue.CooperationContext.LastULIEs, 0x18, [][]byte{{0x01}})
	assertStoredIE(t, ue.CooperationContext.LastULIEs, 0x71, [][]byte{{0xaa, 0xbb}})

	if got := ue.CooperationContext.NegotiatedIEs[0x10]; !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("NegotiatedIEs[0x10] = %x, want 01", got)
	}
	if got := ue.CooperationContext.NegotiatedIEs[0x18]; !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("NegotiatedIEs[0x18] = %x, want 01", got)
	}
	if got := ue.CooperationContext.NegotiatedIEs[0x71]; !bytes.Equal(got, []byte{0xaa, 0xbb}) {
		t.Fatalf("NegotiatedIEs[0x71] = %x, want aabb", got)
	}

	if len(dlIEs) != 2 {
		t.Fatalf("DL IE count = %d, want 2", len(dlIEs))
	}
	assertDLIE(t, dlIEs[0], 0x10, []byte{0x01})
	assertDLIE(t, dlIEs[1], 0x71, []byte{0xaa, 0xbb})
}

func TestProcessULCooperationDoesNotEmitDL18(t *testing.T) {
	ue := &context.AmfUe{}
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 0x01
	mustAddIE(t, ul, 0x18, []byte{0x01})

	dlIEs, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}
	if len(dlIEs) != 0 {
		t.Fatalf("DL IE count = %d, want 0", len(dlIEs))
	}
}

func mustAddIE(t *testing.T, ul *nasMessage.ULCooperation, iei uint8, contents []byte) {
	t.Helper()
	if err := ul.AddIE(iei, contents); err != nil {
		t.Fatalf("AddIE(0x%02x) error = %v", iei, err)
	}
}

func assertStoredIE(t *testing.T, got map[uint8][][]byte, iei uint8, want [][]byte) {
	t.Helper()
	values := got[iei]
	if len(values) != len(want) {
		t.Fatalf("LastULIEs[0x%02x] count = %d, want %d", iei, len(values), len(want))
	}
	for i := range want {
		if !bytes.Equal(values[i], want[i]) {
			t.Fatalf("LastULIEs[0x%02x][%d] = %x, want %x", iei, i, values[i], want[i])
		}
	}
}

func assertDLIE(t *testing.T, ie *nasMessage.CooperationIE, wantIEI uint8, wantContents []byte) {
	t.Helper()
	if ie == nil {
		t.Fatalf("DL IE 0x%02x is nil", wantIEI)
	}
	if ie.GetIei() != wantIEI {
		t.Fatalf("DL IEI = 0x%02x, want 0x%02x", ie.GetIei(), wantIEI)
	}
	if got := ie.GetContents(); !bytes.Equal(got, wantContents) {
		t.Fatalf("DL IE 0x%02x contents = %x, want %x", wantIEI, got, wantContents)
	}
}
