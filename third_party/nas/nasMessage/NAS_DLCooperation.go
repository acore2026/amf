package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/acore2026/nas/nasType"
)

const (
	DLCooperationIE10Type          uint8 = CooperationIEType10
	DLCooperationDLApContainerType uint8 = CooperationIEType71
)

type DLCooperation struct {
	nasType.ExtendedProtocolDiscriminator
	nasType.SpareHalfOctetAndSecurityHeaderType
	MessageType     uint8
	MessageIdentity uint8
	IEs             []*CooperationIE

	// Kept as transitional aliases for code that still inspects the old AP-container shape.
	DLApContainer *CooperationIE
	UnknownIEs    []*CooperationIE
}

type DLCooperationIE = CooperationIE

func NewDLCooperation(messageType uint8) *DLCooperation {
	return &DLCooperation{
		MessageType:     0xe2,
		MessageIdentity: 0x01,
	}
}

func NewDLCooperationIE(iei uint8) *DLCooperationIE {
	return &CooperationIE{Iei: iei}
}

func (a *DLCooperation) AddIE(iei uint8, contents []uint8) error {
	if err := addCooperationIE(&a.IEs, iei, contents); err != nil {
		return err
	}
	a.indexIE(a.IEs[len(a.IEs)-1])
	return nil
}

func (a *DLCooperation) GetIE(iei uint8) *CooperationIE {
	return getCooperationIE(a.IEs, iei)
}

func (a *DLCooperation) GetIEs(iei uint8) []*CooperationIE {
	return getCooperationIEs(a.IEs, iei)
}

func (a *DLCooperation) HasIE(iei uint8) bool {
	return a.GetIE(iei) != nil
}

func (a *DLCooperation) EncodeDLCooperation(buffer *bytes.Buffer) error {
	if err := binary.Write(buffer, binary.BigEndian, a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.MessageType); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/MessageType): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/MessageIdentity): %w", err)
	}
	ies := a.IEs
	if len(ies) == 0 {
		if a.DLApContainer != nil {
			ies = append(ies, a.DLApContainer)
		}
		ies = append(ies, a.UnknownIEs...)
	}
	for _, ie := range ies {
		if err := encodeCooperationIE(buffer, "DLCooperation", ie); err != nil {
			return err
		}
	}
	return nil
}

func (a *DLCooperation) DecodeDLCooperation(byteArray *[]byte) error {
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageType); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/MessageType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/MessageIdentity): %w", err)
	}

	a.IEs = nil
	a.DLApContainer = nil
	a.UnknownIEs = nil
	for buffer.Len() > 0 {
		ie, err := decodeCooperationIE(buffer, "DLCooperation")
		if err != nil {
			return err
		}
		a.IEs = append(a.IEs, ie)
		a.indexIE(ie)
	}
	return nil
}

func (a *DLCooperation) indexIE(ie *CooperationIE) {
	if ie == nil {
		return
	}
	switch ie.GetIei() {
	case DLCooperationDLApContainerType:
		a.DLApContainer = ie
	case DLCooperationIE10Type:
	default:
		a.UnknownIEs = append(a.UnknownIEs, ie)
	}
}
