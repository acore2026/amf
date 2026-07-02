package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/acore2026/nas/nasType"
	"github.com/sirupsen/logrus"
)

type DLCooperation struct {
	nasType.ExtendedProtocolDiscriminator
	nasType.SpareHalfOctetAndSecurityHeaderType
	MessageType    uint8
	MessageIdentity uint8
	DLApContainer  *DLCooperationIE
	UnknownIEs     []*DLCooperationIE
}

type DLCooperationIE struct {
	Iei                    uint8
	Len                    uint16
	ContainerType          uint16
	ContainerContentLength uint16
	ContainerTypePTI       uint8
	ContainerContent       uint32
	Contents               []uint8
}

func NewDLCooperation(messageType uint8) (dLCooperation *DLCooperation) {
	dLCooperation = &DLCooperation{}
	dLCooperation.MessageType = messageType
	dLCooperation.MessageIdentity = 0x02
	return dLCooperation
}

func NewDLCooperationIE(iei uint8) (dLCooperationIE *DLCooperationIE) {
	dLCooperationIE = &DLCooperationIE{}
	dLCooperationIE.SetIei(iei)
	return dLCooperationIE
}

const (
	DLCooperationDLApContainerType uint8 = 0x71
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
	a.Contents = make([]uint8, a.Len)
}

func (a *DLCooperationIE) GetContainerType() (containerType uint16) {
	return a.ContainerType
}

func (a *DLCooperationIE) SetContainerType(containerType uint16) {
	a.ContainerType = containerType
}

func (a *DLCooperationIE) GetContainerContentLength() (length uint16) {
	return a.ContainerContentLength
}

func (a *DLCooperationIE) SetContainerContentLength(length uint16) {
	a.ContainerContentLength = length
}

func (a *DLCooperationIE) GetContainerTypePTI() (pti uint8) {
	return a.ContainerTypePTI
}

func (a *DLCooperationIE) SetContainerTypePTI(pti uint8) {
	a.ContainerTypePTI = pti
}

func (a *DLCooperationIE) GetContainerContent() (content uint32) {
	return a.ContainerContent
}

func (a *DLCooperationIE) SetContainerContent(content uint32) {
	a.ContainerContent = content
}

func (a *DLCooperationIE) GetContents() (contents []uint8) {
	contents = make([]uint8, len(a.Contents))
	copy(contents, a.Contents)
	return contents
}

func (a *DLCooperationIE) SetContents(contents []uint8) {
	a.Contents = make([]uint8, len(contents))
	copy(a.Contents, contents)
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
	
	logrus.WithFields(logrus.Fields{
		"name":            name,
		"iei":             fmt.Sprintf("0x%02x", ie.GetIei()),
		"len":             ie.GetLen(),
		"containerType":   ie.GetContainerType(),
		"contentLength":   ie.GetContainerContentLength(),
		"pti":             ie.GetContainerTypePTI(),
		"content":         ie.GetContainerContent(),
		"contentsLen":     len(ie.Contents),
	}).Debugf("[DLCooperation] Encoding IE")
	
	if err := binary.Write(buffer, binary.BigEndian, ie.GetIei()); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/%s/iei): %w", name, err)
	}
	
	if err := binary.Write(buffer, binary.BigEndian, ie.GetLen()); err != nil {
		return fmt.Errorf("NAS encode error (DLCooperation/%s/len): %w", name, err)
	}
	
	if ie.GetLen() >= 9 {
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerType()); err != nil {
			return fmt.Errorf("NAS encode error (DLCooperation/%s/containerType): %w", name, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerContentLength()); err != nil {
			return fmt.Errorf("NAS encode error (DLCooperation/%s/containerContentLength): %w", name, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerTypePTI()); err != nil {
			return fmt.Errorf("NAS encode error (DLCooperation/%s/containerTypePTI): %w", name, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerContent()); err != nil {
			return fmt.Errorf("NAS encode error (DLCooperation/%s/containerContent): %w", name, err)
		}
	}
	
	if len(ie.Contents) > 0 {
		remainingContents := ie.Contents
		if ie.GetLen() >= 9 {
			if len(ie.Contents) > 9 {
				remainingContents = ie.Contents[9:]
			} else {
				remainingContents = nil
			}
		}
		if len(remainingContents) > 0 {
			if err := binary.Write(buffer, binary.BigEndian, remainingContents); err != nil {
				return fmt.Errorf("NAS encode error (DLCooperation/%s/contents): %w", name, err)
			}
		}
	}
	
	return nil
}

func (a *DLCooperation) DecodeDLCooperation(byteArray *[]byte) error {
	logrus.WithFields(logrus.Fields{
		"len":  len(*byteArray),
		"data": fmt.Sprintf("%x", (*byteArray)[:minInt(len(*byteArray), 128)]),
	}).Debugf("[DLCooperation] DecodeDLCooperation - Full payload")
	
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	logrus.WithField("epd", fmt.Sprintf("0x%02x", a.ExtendedProtocolDiscriminator.Octet)).Debugf("[DLCooperation] Decoded EPD")
	
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageType); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/MessageType): %w", err)
	}
	logrus.WithField("messageType", fmt.Sprintf("0x%02x", a.MessageType)).Debugf("[DLCooperation] Decoded MessageType")
	
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS decode error (DLCooperation/MessageIdentity): %w", err)
	}
	logrus.WithField("messageIdentity", fmt.Sprintf("0x%02x", a.MessageIdentity)).Debugf("[DLCooperation] Decoded MessageIdentity")
	
	logrus.WithField("remainingIEs", buffer.Len()).Debugf("[DLCooperation] Starting IE decoding")
	ieCount := 0
	for buffer.Len() > 0 {
		ie, err := decodeDLCooperationIE(buffer)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"ieCount":   ieCount,
				"remaining": buffer.Len(),
				"data":      fmt.Sprintf("%x", buffer.Bytes()[:minInt(buffer.Len(), 64)]),
			}).Errorf("[DLCooperation] Failed to decode IE: %v", err)
			return err
		}
		ieCount++
		logrus.WithFields(logrus.Fields{
			"ieCount": ieCount,
			"iei":     fmt.Sprintf("0x%02x", ie.GetIei()),
			"ieLen":   ie.GetLen(),
		}).Debugf("[DLCooperation] Successfully decoded IE")
		a.setDLCooperationIE(ie)
	}
	logrus.WithField("totalIEs", ieCount).Debugf("[DLCooperation] Decode completed successfully")
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
	
	remaining := buffer.Len()
	logrus.WithFields(logrus.Fields{
		"iei":       fmt.Sprintf("0x%02x", iei),
		"ieLen":     ie.Len,
		"remaining": remaining,
		"bufferLen": len(buffer.Bytes()),
	}).Debugf("[DLCooperation] Decoding IE")
	
	if int(ie.Len) > remaining {
		logrus.WithFields(logrus.Fields{
			"iei":       fmt.Sprintf("0x%02x", iei),
			"ieLen":     ie.Len,
			"remaining": remaining,
			"data":      fmt.Sprintf("%x", buffer.Bytes()[:minInt(remaining, 64)]),
		}).Errorf("[DLCooperation] IE length exceeds remaining buffer")
		return nil, fmt.Errorf("invalid ie length (DLCooperation/iei 0x%02x): %d exceeds remaining %d", iei, ie.Len, remaining)
	}
	
	ie.Contents = make([]uint8, ie.Len)
	if err := binary.Read(buffer, binary.BigEndian, ie.Contents); err != nil {
		return nil, fmt.Errorf("NAS decode error (DLCooperation/iei 0x%02x): %w", iei, err)
	}
	
	if ie.Len >= 9 {
		containerBuffer := bytes.NewBuffer(ie.Contents)
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerType); err != nil {
			logrus.WithError(err).Warnf("[DLCooperation] Failed to read ContainerType")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerContentLength); err != nil {
			logrus.WithError(err).Warnf("[DLCooperation] Failed to read ContainerContentLength")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerTypePTI); err != nil {
			logrus.WithError(err).Warnf("[DLCooperation] Failed to read ContainerTypePTI")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerContent); err != nil {
			logrus.WithError(err).Warnf("[DLCooperation] Failed to read ContainerContent")
		}
	}
	
	return ie, nil
}

func (a *DLCooperation) setDLCooperationIE(ie *DLCooperationIE) {
	switch ie.GetIei() {
	case DLCooperationDLApContainerType:
		a.DLApContainer = ie
	default:
		a.UnknownIEs = append(a.UnknownIEs, ie)
	}
}