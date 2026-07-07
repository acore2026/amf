package message

import (
	"testing"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

func TestBuildDLCooperationRejectsULOnlyIE18(t *testing.T) {
	ie, err := nasMessage.NewCooperationIE(nasMessage.CooperationIEType18, []byte{0x01})
	if err != nil {
		t.Fatalf("NewCooperationIE() error = %v", err)
	}

	_, err = BuildDLCooperation(
		&context.AmfUe{},
		models.AccessType__3_GPP_ACCESS,
		0x01,
		[]*nasMessage.CooperationIE{ie},
	)
	if err == nil {
		t.Fatal("BuildDLCooperation() error = nil, want IEI 0x18 rejection")
	}
}
