package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/acore2026/nas/nasType"
)

type DLCooperation struct {
	nasType.ExtendedProtocolDiscriminator
	nasType.SpareHalfOctetAndSecurityHeaderType
	nasType.DLCooperationMessageIdentity
	NetworkCapability *DLCooperationIE
	DLApContainer     *DLCooperationIE
	UnknownIEs        []*DLCooperationIE
}

type DLCooperationIE struct {
	Iei    uint8
	Len    uint16
	Buffer []uint8
}

func NewDLCooperation(iei uint8) (dLCooperation *DLCooperation) {
	dLCooperation = &DLCooperation{}
	return dLCooperation
}

func NewDLCooperationIE(iei uint8) (dLCooperationIE *DLCooperationIE) {
	dLCooperationIE = &DLCooperationIE{}
	dLCooperationIE.SetIei(iei)
	return dLCooperationIE
}

const (
	DLCooperationNetworkCapabilityType uint8 = 0x01
	DLCooperationDLApContainerType     uint8 = 0x02
)

func (a *DLCooperationIE) GetIei() (iei uint8) {
	return a.Iei
}

func (a *DLCooperationIE) SetIei(iei uint8) {
	a.Iei = iei
}

func (a *DLCooperationIE) GetLen() (len uint16) {
	return a.Len
}

func (a *DLCooperationIE) SetLen(len uint16) {
	a.Len = len
	a.Buffer = make([]uint8, a.Len)
}

func (a *DLCooperationIE) GetContents() (contents []uint8) {
	contents = make([]uint8, len(a.Buffer))
	copy(contents, a.Buffer)
	return contents
}

func (a *DLCooperationIE) SetContents(contents []uint8) {
	copy(a.Buffer, contents)
}

func (a *DLCooperation) EncodeDLCooperation(buffer *bytes.Buffer) error {
	if err := binary.Write(buffer, binary.BigEndian, a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Write(buffer, binary.BigEndian, a.DLCooperationMessageIdentity.Octet); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/DLCooperationMessageIdentity): %w", err)
	}
	if err := encodeDLCooperationIE(buffer, "NetworkCapability", a.NetworkCapability); err != nil {
		return err
	}
	if err := encodeDLCooperationIE(buffer, "DLApContainer", a.DLApContainer); err != nil {
		return err
	}
	for _, ie := range a.UnknownIEs {
		if err := encodeDLCooperationIE(buffer, "UnknownIE", ie); err != nil {
			return err
		}
	}
	return nil
}

func encodeDLCooperationIE(buffer *bytes.Buffer, name string, ie *DLCooperationIE) error {
	if ie == nil {
		return nil
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.GetIei()); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/%s): %w", name, err)
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.GetLen()); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/%s): %w", name, err)
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Buffer); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/%s): %w", name, err)
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
	if err := binary.Read(buffer, binary.BigEndian, &a.DLCooperationMessageIdentity.Octet); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/DLCooperationMessageIdentity): %w", err)
	}
	for buffer.Len() > 0 {
		ie, err := decodeDLCooperationIE(buffer)
		if err != nil {
			return err
		}
		a.setDLCooperationIE(ie)
	}
	return nil
}

func decodeDLCooperationIE(buffer *bytes.Buffer) (*DLCooperationIE, error) {
	var iei uint8
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		return nil, fmt.Errorf("NAS decode error (DLCooperation/iei): %w", err)
	}
	if buffer.Len() < 2 {
		return nil, fmt.Errorf("NAS decode error (DLCooperation/len): remaining length %d", buffer.Len())
	}
	ie := NewDLCooperationIE(iei)
	if err := binary.Read(buffer, binary.BigEndian, &ie.Len); err != nil {
		return nil, fmt.Errorf("NAS decode error (DLCooperation/len): %w", err)
	}
	if int(ie.Len) > buffer.Len() {
		return nil, fmt.Errorf("invalid ie length (DLCooperation/iei 0x%02x): %d exceeds remaining %d", iei, ie.Len, buffer.Len())
	}
	ie.SetLen(ie.GetLen())
	if err := binary.Read(buffer, binary.BigEndian, ie.Buffer); err != nil {
		return nil, fmt.Errorf("NAS decode error (DLCooperation/iei 0x%02x): %w", iei, err)
	}
	return ie, nil
}

func (a *DLCooperation) setDLCooperationIE(ie *DLCooperationIE) {
	switch ie.GetIei() {
	case DLCooperationNetworkCapabilityType:
		a.NetworkCapability = ie
	case DLCooperationDLApContainerType:
		a.DLApContainer = ie
	default:
		a.assignDLCooperationIEByOrder(ie)
	}
}

func (a *DLCooperation) assignDLCooperationIEByOrder(ie *DLCooperationIE) {
	switch {
	case a.NetworkCapability == nil:
		a.NetworkCapability = ie
	case a.DLApContainer == nil:
		a.DLApContainer = ie
	default:
		a.UnknownIEs = append(a.UnknownIEs, ie)
	}
}