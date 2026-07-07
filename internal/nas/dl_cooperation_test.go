package nas_test

import (
	"bytes"
	"testing"

	acoreNas "github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestEncodeDLCooperationTLVs(t *testing.T) {
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
			ies:  map[uint8][]byte{0x71: {0xaa, 0xbb}},
			want: []byte{nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeDLCooperation, 0x01, 0x71, 0x02, 0xaa, 0xbb},
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
	dl := nasMessage.NewDLCooperation(acoreNas.MsgTypeDLCooperation)
	dl.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
	dl.SpareHalfOctetAndSecurityHeaderType.SetSecurityHeaderType(0)
	dl.MessageIdentity = 0x01
	if err := dl.AddIE(0x10, []byte{0x01}); err != nil {
		t.Fatalf("AddIE(0x10) error = %v", err)
	}
	if err := dl.AddIE(0x71, []byte{0xaa, 0xbb}); err != nil {
		t.Fatalf("AddIE(0x71) error = %v", err)
	}

	buffer := bytes.NewBuffer(nil)
	if err := dl.EncodeDLCooperation(buffer); err != nil {
		t.Fatalf("EncodeDLCooperation() error = %v", err)
	}

	want := []byte{nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeDLCooperation, 0x01, 0x10, 0x01, 0x01, 0x71, 0x02, 0xaa, 0xbb}
	if got := buffer.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("encoded DLCooperation = %x, want %x", got, want)
	}
}
