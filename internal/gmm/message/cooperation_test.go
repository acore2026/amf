package message

import (
	"strings"
	"testing"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/logger"
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

func TestBuildDLCooperationRejectsMultipleAPContainers(t *testing.T) {
	contents, err := (&nasMessage.APContainer{ContainerPayloadID: 1}).Encode()
	if err != nil {
		t.Fatalf("APContainer.Encode() error = %v", err)
	}
	first, err := nasMessage.NewCooperationIE(nasMessage.CooperationIEType71, contents)
	if err != nil {
		t.Fatalf("NewCooperationIE(first) error = %v", err)
	}
	second, err := nasMessage.NewCooperationIE(nasMessage.CooperationIEType71, contents)
	if err != nil {
		t.Fatalf("NewCooperationIE(second) error = %v", err)
	}

	_, err = BuildDLCooperation(
		&context.AmfUe{GmmLog: logger.GmmLog},
		models.AccessType__3_GPP_ACCESS,
		0x01,
		[]*nasMessage.CooperationIE{first, second},
	)
	if err == nil || !strings.Contains(err.Error(), "more than one AP Container") {
		t.Fatalf("BuildDLCooperation() error = %v", err)
	}
}

func TestSendDLCooperationReturnsPreconditionErrors(t *testing.T) {
	if err := SendDLCooperation(nil, 1, nil); err == nil {
		t.Fatal("SendDLCooperation(nil) error = nil")
	}
	if err := SendDLCooperation(&context.RanUe{}, 1, nil); err == nil {
		t.Fatal("SendDLCooperation() error = nil without AmfUe")
	}
	if err := SendDLCooperation(&context.RanUe{
		AmfUe: &context.AmfUe{GmmLog: logger.GmmLog},
	}, 1, nil); err == nil {
		t.Fatal("SendDLCooperation() error = nil without Ran")
	}
}
