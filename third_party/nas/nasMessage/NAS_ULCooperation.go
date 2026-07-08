package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/acore2026/nas/nasType"
)

const (
	ULCooperationIE10Type          uint8 = CooperationIEType10
	ULCooperationIE18Type          uint8 = CooperationIEType18
	ULCooperationULApContainerType uint8 = CooperationIEType71
)

type ULCooperation struct {
	nasType.ExtendedProtocolDiscriminator
	nasType.SpareHalfOctetAndSecurityHeaderType
	MessageType     uint8
	MessageIdentity uint8
	IEs             []*CooperationIE

	// Kept as transitional aliases for code that still inspects the old AP-container shape.
	ULApContainer *CooperationIE
	UnknownIEs    []*CooperationIE
}

type ULCooperationIE = CooperationIE

func NewULCooperation(messageType uint8) *ULCooperation {
	return &ULCooperation{
		MessageType:     messageType,
		MessageIdentity: 0x01,
	}
}

func NewULCooperationIE(iei uint8) *ULCooperationIE {
	return &CooperationIE{Iei: iei}
}

func (a *ULCooperation) AddIE(iei uint8, contents []uint8) error {
	if err := addCooperationIE(&a.IEs, iei, contents); err != nil {
		return err
	}
	a.indexIE(a.IEs[len(a.IEs)-1])
	return nil
}

func (a *ULCooperation) GetIE(iei uint8) *CooperationIE {
	return getCooperationIE(a.IEs, iei)
}

func (a *ULCooperation) GetIEs(iei uint8) []*CooperationIE {
	return getCooperationIEs(a.IEs, iei)
}

func (a *ULCooperation) HasIE(iei uint8) bool {
	return a.GetIE(iei) != nil
}

func (a *ULCooperation) EncodeULCooperation(buffer *bytes.Buffer) error {
	if err := binary.Write(buffer, binary.BigEndian, a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.MessageType); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/MessageType): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/MessageIdentity): %w", err)
	}
	ies := a.IEs
	if len(ies) == 0 {
		if a.ULApContainer != nil {
			ies = append(ies, a.ULApContainer)
		}
		ies = append(ies, a.UnknownIEs...)
	}
	for _, ie := range ies {
		if err := encodeCooperationIE(buffer, "ULCooperation", ie); err != nil {
			return err
		}
	}
	return nil
}

func (a *ULCooperation) DecodeULCooperation(byteArray *[]byte) error {
	return a.decode(byteArray)
}

func (a *ULCooperation) DecodeULCooperationV2(byteArray *[]byte) error {
	return a.decode(byteArray)
}

func (a *ULCooperation) decode(byteArray *[]byte) error {
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageType); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/MessageType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/MessageIdentity): %w", err)
	}

	a.IEs = nil
	a.ULApContainer = nil
	a.UnknownIEs = nil
	// MessageIdentity == 0x01 uses new TLV format (1-byte length)
	// Other values use legacy AP Container format (2-byte uint16 length)
	legacy := a.MessageIdentity != 0x01
	for buffer.Len() > 0 {
		var ie *CooperationIE
		var err error
		if legacy {
			ie, err = decodeCooperationIELegacy(buffer, "ULCooperation")
		} else {
			ie, err = decodeCooperationIE(buffer, "ULCooperation")
		}
		if err != nil {
			return err
		}
		a.IEs = append(a.IEs, ie)
		a.indexIE(ie)
	}
	return nil
}

func (a *ULCooperation) indexIE(ie *CooperationIE) {
	if ie == nil {
		return
	}
	switch ie.GetIei() {
	case ULCooperationULApContainerType:
		a.ULApContainer = ie
	case ULCooperationIE10Type, ULCooperationIE18Type:
	default:
		a.UnknownIEs = append(a.UnknownIEs, ie)
	}
}
