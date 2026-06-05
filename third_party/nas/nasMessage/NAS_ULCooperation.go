package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/acore2026/nas/nasType"
)

type ULCooperation struct {
	nasType.ExtendedProtocolDiscriminator
	nasType.SpareHalfOctetAndSecurityHeaderType
	messageType          uint8
	nasType.ULCooperationMessageIdentity
	ULApContainer       *ULCooperationIE
	UnknownIEs          []*ULCooperationIE
}

type ULCooperationIE struct {
	Iei    uint8
	Len    uint16
	Buffer []uint8
}

func NewULCooperation(iei uint8) (uLCooperation *ULCooperation) {
	uLCooperation = &ULCooperation{}
	uLCooperation.messageType = iei
	uLCooperation.ULCooperationMessageIdentity.SetMessageType(iei)
	return uLCooperation
}

func NewULCooperationIE(iei uint8) (uLCooperationIE *ULCooperationIE) {
	uLCooperationIE = &ULCooperationIE{}
	uLCooperationIE.SetIei(iei)
	return uLCooperationIE
}

const (
	ULCooperationULApContainerType uint8 = 0x03
)

func (a *ULCooperation) GetMessageType() (messageType uint8) {
	return a.messageType
}

func (a *ULCooperation) SetMessageType(messageType uint8) {
	a.messageType = messageType
	a.ULCooperationMessageIdentity.SetMessageType(messageType)
}

func (a *ULCooperationIE) GetIei() (iei uint8) {
	return a.Iei
}

func (a *ULCooperationIE) SetIei(iei uint8) {
	a.Iei = iei
}

func (a *ULCooperationIE) GetLen() (len uint16) {
	return a.Len
}

func (a *ULCooperationIE) SetLen(len uint16) {
	a.Len = len
	a.Buffer = make([]uint8, a.Len)
}

func (a *ULCooperationIE) GetContents() (contents []uint8) {
	contents = make([]uint8, len(a.Buffer))
	copy(contents, a.Buffer)
	return contents
}

func (a *ULCooperationIE) SetContents(contents []uint8) {
	copy(a.Buffer, contents)
}

func (a *ULCooperation) EncodeULCooperation(buffer *bytes.Buffer) error {
	if err := binary.Write(buffer, binary.BigEndian, a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.ULCooperationMessageIdentity.Octet); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/ULCooperationMessageIdentity): %w", err)
	}
	if err := encodeULCooperationIE(buffer, "ULApContainer", a.ULApContainer); err != nil {
		return err
	}
	for _, ie := range a.UnknownIEs {
		if err := encodeULCooperationIE(buffer, "UnknownIE", ie); err != nil {
			return err
		}
	}
	return nil
}

func encodeULCooperationIE(buffer *bytes.Buffer, name string, ie *ULCooperationIE) error {
	if ie == nil {
		return nil
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.GetIei()); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/%s): %w", name, err)
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.GetLen()); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/%s): %w", name, err)
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Buffer); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/%s): %w", name, err)
	}
	return nil
}

func (a *ULCooperation) DecodeULCooperation(byteArray *[]byte) error {
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.ULCooperationMessageIdentity.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ULCooperationMessageIdentity): %w", err)
	}
	a.messageType = a.ULCooperationMessageIdentity.GetMessageType()
	for buffer.Len() > 0 {
		ie, err := decodeULCooperationIE(buffer)
		if err != nil {
			return err
		}
		a.setULCooperationIE(ie)
	}
	return nil
}

func (a *ULCooperation) DecodeULCooperationV2(byteArray *[]byte) error {
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.ULCooperationMessageIdentity.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ULCooperationMessageIdentity): %w", err)
	}
	a.messageType = a.ULCooperationMessageIdentity.GetMessageType()
	for buffer.Len() > 0 {
		ie, err := decodeULCooperationIEV2(buffer)
		if err != nil {
			return err
		}
		a.setULCooperationIE(ie)
	}
	return nil
}

func decodeULCooperationIEV2(buffer *bytes.Buffer) (*ULCooperationIE, error) {
	var iei uint8
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei): %w", err)
	}
	var len1 uint8
	if err := binary.Read(buffer, binary.BigEndian, &len1); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/len): %w", err)
	}
	ie := NewULCooperationIE(iei)
	ie.Len = uint16(len1)
	if int(ie.Len) > buffer.Len() {
		return nil, fmt.Errorf("invalid ie length (ULCooperation/iei 0x%02x): %d exceeds remaining %d", iei, ie.Len, buffer.Len())
	}
	ie.Buffer = make([]uint8, ie.Len)
	if err := binary.Read(buffer, binary.BigEndian, ie.Buffer); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei 0x%02x): %w", iei, err)
	}
	return ie, nil
}

func decodeULCooperationIE(buffer *bytes.Buffer) (*ULCooperationIE, error) {
	var iei uint8
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei): %w", err)
	}
	if buffer.Len() < 2 {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/len): remaining length %d", buffer.Len())
	}
	ie := NewULCooperationIE(iei)
	if err := binary.Read(buffer, binary.BigEndian, &ie.Len); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/len): %w", err)
	}
	if int(ie.Len) > buffer.Len() {
		return nil, fmt.Errorf("invalid ie length (ULCooperation/iei 0x%02x): %d exceeds remaining %d", iei, ie.Len, buffer.Len())
	}
	ie.SetLen(ie.GetLen())
	if err := binary.Read(buffer, binary.BigEndian, ie.Buffer); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei 0x%02x): %w", iei, err)
	}
	return ie, nil
}

func (a *ULCooperation) setULCooperationIE(ie *ULCooperationIE) {
	switch ie.GetIei() {
	case ULCooperationULApContainerType:
		a.ULApContainer = ie
	default:
		a.UnknownIEs = append(a.UnknownIEs, ie)
	}
}