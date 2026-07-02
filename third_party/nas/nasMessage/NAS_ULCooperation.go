package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/acore2026/nas/nasType"
	"github.com/sirupsen/logrus"
)

type ULCooperation struct {
	nasType.ExtendedProtocolDiscriminator
	nasType.SpareHalfOctetAndSecurityHeaderType
	MessageType    uint8
	MessageIdentity uint8
	ULApContainer  *ULCooperationIE
	UnknownIEs     []*ULCooperationIE
}

type ULCooperationIE struct {
	Iei                    uint8
	Len                    uint16
	ContainerType          uint16
	ContainerContentLength uint16
	ContainerTypePTI       uint8
	ContainerContent       uint32
	Contents               []uint8
}

func NewULCooperation(messageType uint8) (uLCooperation *ULCooperation) {
	uLCooperation = &ULCooperation{}
	uLCooperation.MessageType = messageType
	uLCooperation.MessageIdentity = messageType // 默认值，可根据需要修改
	return uLCooperation
}

func NewULCooperationIE(iei uint8) (uLCooperationIE *ULCooperationIE) {
	uLCooperationIE = &ULCooperationIE{}
	uLCooperationIE.SetIei(iei)
	return uLCooperationIE
}

const (
	ULCooperationULApContainerType uint8 = 0x71
)

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
	a.Contents = make([]uint8, a.Len)
}

func (a *ULCooperationIE) GetContainerType() (containerType uint16) {
	return a.ContainerType
}

func (a *ULCooperationIE) SetContainerType(containerType uint16) {
	a.ContainerType = containerType
}

func (a *ULCooperationIE) GetContainerContentLength() (length uint16) {
	return a.ContainerContentLength
}

func (a *ULCooperationIE) SetContainerContentLength(length uint16) {
	a.ContainerContentLength = length
}

func (a *ULCooperationIE) GetContainerTypePTI() (pti uint8) {
	return a.ContainerTypePTI
}

func (a *ULCooperationIE) SetContainerTypePTI(pti uint8) {
	a.ContainerTypePTI = pti
}

func (a *ULCooperationIE) GetContainerContent() (content uint32) {
	return a.ContainerContent
}

func (a *ULCooperationIE) SetContainerContent(content uint32) {
	a.ContainerContent = content
}

func (a *ULCooperationIE) GetContents() (contents []uint8) {
	contents = make([]uint8, len(a.Contents))
	copy(contents, a.Contents)
	return contents
}

func (a *ULCooperationIE) SetContents(contents []uint8) {
	a.Contents = make([]uint8, len(contents))
	copy(a.Contents, contents)
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
	
	logrus.WithFields(logrus.Fields{
		"name":            name,
		"iei":             fmt.Sprintf("0x%02x", ie.GetIei()),
		"len":             ie.GetLen(),
		"containerType":   ie.GetContainerType(),
		"contentLength":   ie.GetContainerContentLength(),
		"pti":             ie.GetContainerTypePTI(),
		"content":         ie.GetContainerContent(),
		"contentsLen":     len(ie.Contents),
	}).Debugf("[ULCooperation] Encoding IE")
	
	// Encode IEI
	if err := binary.Write(buffer, binary.BigEndian, ie.GetIei()); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/%s/iei): %w", name, err)
	}
	
	// Encode Len
	if err := binary.Write(buffer, binary.BigEndian, ie.GetLen()); err != nil {
		return fmt.Errorf("NAS encode error (ULCooperation/%s/len): %w", name, err)
	}
	
	// Encode Container fields
	if ie.GetLen() >= 9 { // Minimum size for container fields (2+2+1+4=9 bytes)
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerType()); err != nil {
			return fmt.Errorf("NAS encode error (ULCooperation/%s/containerType): %w", name, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerContentLength()); err != nil {
			return fmt.Errorf("NAS encode error (ULCooperation/%s/containerContentLength): %w", name, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerTypePTI()); err != nil {
			return fmt.Errorf("NAS encode error (ULCooperation/%s/containerTypePTI): %w", name, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, ie.GetContainerContent()); err != nil {
			return fmt.Errorf("NAS encode error (ULCooperation/%s/containerContent): %w", name, err)
		}
	}
	
	// Encode remaining Contents (if any beyond the fixed fields)
	if len(ie.Contents) > 0 {
		remainingContents := ie.Contents
		if ie.GetLen() >= 9 {
			// Skip first 9 bytes which are already encoded as container fields
			if len(ie.Contents) > 9 {
				remainingContents = ie.Contents[9:]
			} else {
				remainingContents = nil
			}
		}
		if len(remainingContents) > 0 {
			if err := binary.Write(buffer, binary.BigEndian, remainingContents); err != nil {
				return fmt.Errorf("NAS encode error (ULCooperation/%s/contents): %w", name, err)
			}
		}
	}
	
	return nil
}

func (a *ULCooperation) DecodeULCooperation(byteArray *[]byte) error {
	logrus.WithFields(logrus.Fields{
		"len":  len(*byteArray),
		"data": fmt.Sprintf("%x", (*byteArray)[:minInt(len(*byteArray), 128)]),
	}).Debugf("[ULCooperation] DecodeULCooperation - Full payload")
	
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	logrus.WithField("epd", fmt.Sprintf("0x%02x", a.ExtendedProtocolDiscriminator.Octet)).Debugf("[ULCooperation] Decoded EPD")
	
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageType); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/MessageType): %w", err)
	}
	logrus.WithField("messageType", fmt.Sprintf("0x%02x", a.MessageType)).Debugf("[ULCooperation] Decoded MessageType")
	
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/MessageIdentity): %w", err)
	}
	logrus.WithField("messageIdentity", fmt.Sprintf("0x%02x", a.MessageIdentity)).Debugf("[ULCooperation] Decoded MessageIdentity")
	
	logrus.WithField("remainingIEs", buffer.Len()).Debugf("[ULCooperation] Starting IE decoding")
	ieCount := 0
	for buffer.Len() > 0 {
		ie, err := decodeULCooperationIE(buffer)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"ieCount":   ieCount,
				"remaining": buffer.Len(),
				"data":      fmt.Sprintf("%x", buffer.Bytes()[:minInt(buffer.Len(), 64)]),
			}).Errorf("[ULCooperation] Failed to decode IE: %v", err)
			return err
		}
		ieCount++
		logrus.WithFields(logrus.Fields{
			"ieCount": ieCount,
			"iei":     fmt.Sprintf("0x%02x", ie.GetIei()),
			"ieLen":   ie.GetLen(),
		}).Debugf("[ULCooperation] Successfully decoded IE")
		a.setULCooperationIE(ie)
	}
	logrus.WithField("totalIEs", ieCount).Debugf("[ULCooperation] Decode completed successfully")
	return nil
}

func (a *ULCooperation) DecodeULCooperationV2(byteArray *[]byte) error {
	logrus.WithFields(logrus.Fields{
		"len":       len(*byteArray),
		"data":      fmt.Sprintf("%x", *byteArray),
		"data128":   fmt.Sprintf("%x", (*byteArray)[:minInt(len(*byteArray), 128)]),
	}).Infof("[ULCooperation] DecodeULCooperationV2 - Full payload")
	
	buffer := bytes.NewBuffer(*byteArray)
	if err := binary.Read(buffer, binary.BigEndian, &a.ExtendedProtocolDiscriminator.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/ExtendedProtocolDiscriminator): %w", err)
	}
	logrus.WithField("epd", fmt.Sprintf("0x%02x", a.ExtendedProtocolDiscriminator.Octet)).Infof("[ULCooperation] Decoded EPD")
	
	if err := binary.Read(buffer, binary.BigEndian, &a.SpareHalfOctetAndSecurityHeaderType.Octet); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/SpareHalfOctetAndSecurityHeaderType): %w", err)
	}
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageType); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/MessageType): %w", err)
	}
	logrus.WithField("messageType", fmt.Sprintf("0x%02x", a.MessageType)).Infof("[ULCooperation] Decoded MessageType")
	
	if err := binary.Read(buffer, binary.BigEndian, &a.MessageIdentity); err != nil {
		return fmt.Errorf("NAS decode error (ULCooperation/MessageIdentity): %w", err)
	}
	logrus.WithField("messageIdentity", fmt.Sprintf("0x%02x", a.MessageIdentity)).Infof("[ULCooperation] Decoded MessageIdentity")
	
	logrus.WithField("remainingIEs", buffer.Len()).Infof("[ULCooperation] Starting IE decoding")
	ieCount := 0
	for buffer.Len() > 0 {
		ie, err := decodeULCooperationIEV2(buffer)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"ieCount":   ieCount,
				"remaining": buffer.Len(),
				"data":      fmt.Sprintf("%x", buffer.Bytes()),
				"data64":    fmt.Sprintf("%x", buffer.Bytes()[:minInt(buffer.Len(), 64)]),
			}).Errorf("[ULCooperation] Failed to decode IE: %v", err)
			return err
		}
		ieCount++
		logrus.WithFields(logrus.Fields{
			"ieCount": ieCount,
			"iei":     fmt.Sprintf("0x%02x", ie.GetIei()),
			"ieLen":   ie.GetLen(),
		}).Infof("[ULCooperation] Successfully decoded IE")
		a.setULCooperationIE(ie)
	}
	logrus.WithField("totalIEs", ieCount).Infof("[ULCooperation] Decode completed successfully")
	return nil
}

func decodeULCooperationIEV2(buffer *bytes.Buffer) (*ULCooperationIE, error) {
	logrus.WithFields(logrus.Fields{
		"remaining": buffer.Len(),
		"data":      fmt.Sprintf("%x", buffer.Bytes()),
		"data64":    fmt.Sprintf("%x", buffer.Bytes()[:min(buffer.Len(), 64)]),
	}).Infof("[ULCooperation] decodeULCooperationIEV2 - Starting to decode IE")
	
	var iei uint8
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei): %w", err)
	}
	logrus.WithField("iei", fmt.Sprintf("0x%02x", iei)).Infof("[ULCooperation] Read IEI byte")
	
	if buffer.Len() < 2 {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/len): remaining length %d", buffer.Len())
	}
	
	ie := NewULCooperationIE(iei)
	if err := binary.Read(buffer, binary.BigEndian, &ie.Len); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/len): %w", err)
	}
	logrus.WithField("lenBytes", fmt.Sprintf("0x%04x (decimal: %d)", ie.Len, ie.Len)).Infof("[ULCooperation] Read Length (V2 uint16)")
	
	remaining := buffer.Len()
	logrus.WithFields(logrus.Fields{
		"iei":       fmt.Sprintf("0x%02x", iei),
		"ieLen":     ie.Len,
		"remaining": remaining,
		"bufferLen": len(buffer.Bytes()),
	}).Infof("[ULCooperation] Decoding IE V2")
	
	originalLen := ie.Len
	
	if int(ie.Len) > remaining {
		logrus.WithFields(logrus.Fields{
			"iei":           fmt.Sprintf("0x%02x", iei),
			"originalLen":   originalLen,
			"remaining":     remaining,
			"data":          fmt.Sprintf("%x", buffer.Bytes()),
			"data64":        fmt.Sprintf("%x", buffer.Bytes()[:minInt(remaining, 64)]),
		}).Errorf("[ULCooperation] IE length exceeds remaining buffer")
		
		// 容错处理：使用实际remaining长度
		if remaining > 0 {
			ie.Len = uint16(remaining)
			logrus.WithFields(logrus.Fields{
				"iei":         fmt.Sprintf("0x%02x", iei),
				"originalLen": originalLen,
				"adjustedLen": ie.Len,
			}).Warnf("[ULCooperation] Adjusted IE length from %d to %d for IEI 0x%02x", originalLen, ie.Len, iei)
		} else {
			return nil, fmt.Errorf("invalid ie length (ULCooperation/iei 0x%02x): %d exceeds remaining %d", iei, originalLen, remaining)
		}
	}
	
	// Read all contents into buffer first
	ie.Contents = make([]uint8, ie.Len)
	if err := binary.Read(buffer, binary.BigEndian, ie.Contents); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei 0x%02x): %w", iei, err)
	}
	
	logrus.WithFields(logrus.Fields{
		"iei":      fmt.Sprintf("0x%02x", iei),
		"ieLen":    ie.Len,
		"contents": fmt.Sprintf("%x", ie.Contents),
	}).Infof("[ULCooperation] Successfully read IE contents")
	
	// Parse container fields from contents if length >= 9
	if ie.Len >= 9 {
		containerBuffer := bytes.NewBuffer(ie.Contents)
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerType); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerType")
		} else {
			logrus.WithField("containerType", fmt.Sprintf("0x%04x", ie.ContainerType)).Infof("[ULCooperation] Decoded ContainerType")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerContentLength); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerContentLength")
		} else {
			logrus.WithField("containerContentLength", ie.ContainerContentLength).Infof("[ULCooperation] Decoded ContainerContentLength")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerTypePTI); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerTypePTI")
		} else {
			logrus.WithField("containerTypePTI", fmt.Sprintf("0x%02x", ie.ContainerTypePTI)).Infof("[ULCooperation] Decoded ContainerTypePTI")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerContent); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerContent")
		} else {
			logrus.WithField("containerContent", fmt.Sprintf("0x%08x", ie.ContainerContent)).Infof("[ULCooperation] Decoded ContainerContent")
		}
		
		logrus.WithFields(logrus.Fields{
			"iei":                    fmt.Sprintf("0x%02x", iei),
			"containerType":          fmt.Sprintf("0x%04x", ie.ContainerType),
			"containerContentLength": ie.ContainerContentLength,
			"containerTypePTI":       fmt.Sprintf("0x%02x", ie.ContainerTypePTI),
			"containerContent":       fmt.Sprintf("0x%08x", ie.ContainerContent),
		}).Infof("[ULCooperation] Decoded container fields")
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
	
	remaining := buffer.Len()
	logrus.WithFields(logrus.Fields{
		"iei":       fmt.Sprintf("0x%02x", iei),
		"ieLen":     ie.Len,
		"remaining": remaining,
		"bufferLen": len(buffer.Bytes()),
	}).Debugf("[ULCooperation] Decoding IE")
	
	if int(ie.Len) > remaining {
		logrus.WithFields(logrus.Fields{
			"iei":       fmt.Sprintf("0x%02x", iei),
			"ieLen":     ie.Len,
			"remaining": remaining,
			"data":      fmt.Sprintf("%x", buffer.Bytes()[:min(remaining, 64)]),
		}).Errorf("[ULCooperation] IE length exceeds remaining buffer")
		return nil, fmt.Errorf("invalid ie length (ULCooperation/iei 0x%02x): %d exceeds remaining %d", iei, ie.Len, remaining)
	}
	
	// Read all contents
	ie.Contents = make([]uint8, ie.Len)
	if err := binary.Read(buffer, binary.BigEndian, ie.Contents); err != nil {
		return nil, fmt.Errorf("NAS decode error (ULCooperation/iei 0x%02x): %w", iei, err)
	}
	
	// Parse container fields if length >= 9
	if ie.Len >= 9 {
		containerBuffer := bytes.NewBuffer(ie.Contents)
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerType); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerType")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerContentLength); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerContentLength")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerTypePTI); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerTypePTI")
		}
		
		if err := binary.Read(containerBuffer, binary.BigEndian, &ie.ContainerContent); err != nil {
			logrus.WithError(err).Warnf("[ULCooperation] Failed to read ContainerContent")
		}
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
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
