package nasMessage

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	CooperationIEType10 uint8 = 0x10
	CooperationIEType18 uint8 = 0x18
	CooperationIEType71 uint8 = 0x71
)

type CooperationIE struct {
	Iei       uint8
	Len       uint8
	LegacyLen uint16 // non-zero when using legacy 2-byte length format
	Contents  []uint8
}

func NewCooperationIE(iei uint8, contents []uint8) (*CooperationIE, error) {
	if len(contents) > 255 {
		return nil, fmt.Errorf("cooperation IE 0x%02x length %d exceeds 255", iei, len(contents))
	}
	ie := &CooperationIE{Iei: iei}
	ie.SetContents(contents)
	return ie, nil
}

func NewCooperationIELegacy(iei uint8, contents []uint8) *CooperationIE {
	ie := &CooperationIE{
		Iei:       iei,
		LegacyLen: uint16(len(contents)),
		Contents:  make([]uint8, len(contents)),
	}
	copy(ie.Contents, contents)
	if len(contents) <= 255 {
		ie.Len = uint8(len(contents))
	}
	return ie
}

func (a *CooperationIE) GetIei() uint8 {
	return a.Iei
}

func (a *CooperationIE) SetIei(iei uint8) {
	a.Iei = iei
}

func (a *CooperationIE) GetLen() uint8 {
	return a.Len
}

func (a *CooperationIE) SetLen(length uint8) {
	a.Len = length
	a.Contents = make([]uint8, length)
}

func (a *CooperationIE) GetContents() []uint8 {
	contents := make([]uint8, len(a.Contents))
	copy(contents, a.Contents)
	return contents
}

func (a *CooperationIE) SetContents(contents []uint8) {
	a.Len = uint8(len(contents))
	a.Contents = make([]uint8, len(contents))
	copy(a.Contents, contents)
}

func (a *CooperationIE) GetContainerType() uint16 {
	if len(a.Contents) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(a.Contents[0:2])
}

func (a *CooperationIE) SetContainerType(containerType uint16) {
	a.ensureContentsLen(2)
	binary.BigEndian.PutUint16(a.Contents[0:2], containerType)
}

func (a *CooperationIE) GetContainerContentLength() uint16 {
	if len(a.Contents) < 4 {
		return 0
	}
	return binary.BigEndian.Uint16(a.Contents[2:4])
}

func (a *CooperationIE) SetContainerContentLength(length uint16) {
	a.ensureContentsLen(4)
	binary.BigEndian.PutUint16(a.Contents[2:4], length)
}

func (a *CooperationIE) GetContainerTypePTI() uint8 {
	if len(a.Contents) < 5 {
		return 0
	}
	return a.Contents[4]
}

func (a *CooperationIE) SetContainerTypePTI(pti uint8) {
	a.ensureContentsLen(5)
	a.Contents[4] = pti
	a.Len = uint8(len(a.Contents))
}

func (a *CooperationIE) GetContainerContent() uint32 {
	if len(a.Contents) < 9 {
		return 0
	}
	return binary.BigEndian.Uint32(a.Contents[5:9])
}

func (a *CooperationIE) SetContainerContent(content uint32) {
	a.ensureContentsLen(9)
	binary.BigEndian.PutUint32(a.Contents[5:9], content)
}

func (a *CooperationIE) ensureContentsLen(length int) {
	if len(a.Contents) >= length {
		return
	}
	contents := make([]uint8, length)
	copy(contents, a.Contents)
	a.Contents = contents
	a.Len = uint8(len(contents))
}

func encodeCooperationIE(buffer *bytes.Buffer, messageName string, ie *CooperationIE) error {
	if ie == nil {
		return nil
	}
	if int(ie.Len) != len(ie.Contents) {
		return fmt.Errorf("NAS encode error (%s/iei 0x%02x): length %d does not match contents length %d",
			messageName, ie.Iei, ie.Len, len(ie.Contents))
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Iei); err != nil {
		return fmt.Errorf("NAS encode error (%s/iei): %w", messageName, err)
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Len); err != nil {
		return fmt.Errorf("NAS encode error (%s/len): %w", messageName, err)
	}
	if ie.Len == 0 {
		return nil
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Contents); err != nil {
		return fmt.Errorf("NAS encode error (%s/contents): %w", messageName, err)
	}
	return nil
}

func decodeCooperationIE(buffer *bytes.Buffer, messageName string) (*CooperationIE, error) {
	if buffer.Len() < 2 {
		return nil, fmt.Errorf("NAS decode error (%s/IE): remaining length %d", messageName, buffer.Len())
	}
	var iei uint8
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		return nil, fmt.Errorf("NAS decode error (%s/iei): %w", messageName, err)
	}
	var length uint8
	if err := binary.Read(buffer, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("NAS decode error (%s/len): %w", messageName, err)
	}
	if int(length) > buffer.Len() {
		return nil, fmt.Errorf("invalid IE length (%s/iei 0x%02x): %d exceeds remaining %d",
			messageName, iei, length, buffer.Len())
	}
	contents := make([]uint8, length)
	if length > 0 {
		if err := binary.Read(buffer, binary.BigEndian, contents); err != nil {
			return nil, fmt.Errorf("NAS decode error (%s/iei 0x%02x): %w", messageName, iei, err)
		}
	}
	return &CooperationIE{
		Iei:      iei,
		Len:      length,
		Contents: contents,
	}, nil
}

func addCooperationIE(ies *[]*CooperationIE, iei uint8, contents []uint8) error {
	ie, err := NewCooperationIE(iei, contents)
	if err != nil {
		return err
	}
	*ies = append(*ies, ie)
	return nil
}

func getCooperationIE(ies []*CooperationIE, iei uint8) *CooperationIE {
	for _, ie := range ies {
		if ie != nil && ie.Iei == iei {
			return ie
		}
	}
	return nil
}

func getCooperationIEs(ies []*CooperationIE, iei uint8) []*CooperationIE {
	matches := make([]*CooperationIE, 0)
	for _, ie := range ies {
		if ie != nil && ie.Iei == iei {
			matches = append(matches, ie)
		}
	}
	return matches
}

// --- Legacy AP Container format (2-byte uint16 length) ---

func encodeCooperationIELegacy(buffer *bytes.Buffer, messageName string, ie *CooperationIE) error {
	if ie == nil {
		return nil
	}
	legacyLen := ie.LegacyLen
	if legacyLen == 0 {
		legacyLen = uint16(len(ie.Contents))
	}
	if int(legacyLen) != len(ie.Contents) && ie.LegacyLen != 0 {
		return fmt.Errorf("NAS encode error (%s/iei 0x%02x): legacy length %d does not match contents length %d",
			messageName, ie.Iei, legacyLen, len(ie.Contents))
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Iei); err != nil {
		return fmt.Errorf("NAS encode error (%s/iei): %w", messageName, err)
	}
	if err := binary.Write(buffer, binary.BigEndian, legacyLen); err != nil {
		return fmt.Errorf("NAS encode error (%s/len): %w", messageName, err)
	}
	if legacyLen == 0 {
		return nil
	}
	if err := binary.Write(buffer, binary.BigEndian, ie.Contents); err != nil {
		return fmt.Errorf("NAS encode error (%s/contents): %w", messageName, err)
	}
	return nil
}

func decodeCooperationIELegacy(buffer *bytes.Buffer, messageName string) (*CooperationIE, error) {
	if buffer.Len() < 3 {
		return nil, fmt.Errorf("NAS decode error (%s/legacy IE): remaining length %d", messageName, buffer.Len())
	}
	var iei uint8
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		return nil, fmt.Errorf("NAS decode error (%s/iei): %w", messageName, err)
	}
	var length uint16
	if err := binary.Read(buffer, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("NAS decode error (%s/len): %w", messageName, err)
	}
	if int(length) > buffer.Len() {
		return nil, fmt.Errorf("invalid IE length (%s/iei 0x%02x): %d exceeds remaining %d",
			messageName, iei, length, buffer.Len())
	}
	contents := make([]uint8, length)
	if length > 0 {
		if err := binary.Read(buffer, binary.BigEndian, contents); err != nil {
			return nil, fmt.Errorf("NAS decode error (%s/iei 0x%02x): %w", messageName, iei, err)
		}
	}
	ie := &CooperationIE{
		Iei:       iei,
		LegacyLen: length,
		Contents:  contents,
	}
	if length <= 255 {
		ie.Len = uint8(length)
	}
	return ie, nil
}
