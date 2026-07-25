package nasMessage

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	APContainerHeaderLength                      = 10
	APContainerFlagDF                      uint8 = 0x02
	APContainerFlagMF                      uint8 = 0x04
	APContainerKnownFlags                  uint8 = APContainerFlagDF | APContainerFlagMF
	APContainerMaxReassembledPayloadLength       = 65535
	APContainerMaxDLFragmentSize                 = 1400
)

type APContainer struct {
	ContainerType      uint16
	ContainerTypePTI   uint8
	ContainerPayloadID uint16
	ContainerFlags     uint8
	FragmentOffset     uint16
	Payload            []byte
}

func (a *APContainer) DontFragment() bool {
	return a != nil && a.ContainerFlags&APContainerFlagDF != 0
}

func (a *APContainer) MoreFragments() bool {
	return a != nil && a.ContainerFlags&APContainerFlagMF != 0
}

func (a *APContainer) Encode() ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("AP Container is nil")
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	if len(a.Payload) > math.MaxUint16-6 {
		return nil, fmt.Errorf("AP Container fragment payload length %d exceeds inner length capacity",
			len(a.Payload))
	}
	encoded := make([]byte, APContainerHeaderLength+len(a.Payload))
	binary.BigEndian.PutUint16(encoded[0:2], a.ContainerType)
	binary.BigEndian.PutUint16(encoded[2:4], uint16(6+len(a.Payload)))
	encoded[4] = a.ContainerTypePTI
	binary.BigEndian.PutUint16(encoded[5:7], a.ContainerPayloadID)
	encoded[7] = a.ContainerFlags
	binary.BigEndian.PutUint16(encoded[8:10], a.FragmentOffset)
	copy(encoded[10:], a.Payload)
	return encoded, nil
}

func DecodeAPContainer(contents []byte) (*APContainer, error) {
	if len(contents) < APContainerHeaderLength {
		return nil, fmt.Errorf("AP Container length %d is less than header length %d",
			len(contents), APContainerHeaderLength)
	}
	contentLength := binary.BigEndian.Uint16(contents[2:4])
	if contentLength < 6 || int(contentLength) != len(contents)-4 {
		return nil, fmt.Errorf("AP Container content length %d does not match value length %d",
			contentLength, len(contents))
	}
	container := &APContainer{
		ContainerType:      binary.BigEndian.Uint16(contents[0:2]),
		ContainerTypePTI:   contents[4],
		ContainerPayloadID: binary.BigEndian.Uint16(contents[5:7]),
		ContainerFlags:     contents[7],
		FragmentOffset:     binary.BigEndian.Uint16(contents[8:10]),
		Payload:            append([]byte(nil), contents[10:]...),
	}
	if err := container.validate(); err != nil {
		return nil, err
	}
	return container, nil
}

func (a *APContainer) validate() error {
	if a.ContainerFlags&^APContainerKnownFlags != 0 {
		return fmt.Errorf("AP Container reserved flags are non-zero: 0x%02x", a.ContainerFlags)
	}
	if a.DontFragment() && (a.MoreFragments() || a.FragmentOffset != 0) {
		return fmt.Errorf("AP Container DF requires MF=0 and offset=0")
	}
	if len(a.Payload) == 0 && (a.MoreFragments() || a.FragmentOffset != 0) {
		return fmt.Errorf("AP Container empty payload requires MF=0 and offset=0")
	}
	fragmentEnd := int(a.FragmentOffset) + len(a.Payload)
	if fragmentEnd > APContainerMaxReassembledPayloadLength {
		return fmt.Errorf("AP Container fragment end %d exceeds %d",
			fragmentEnd, APContainerMaxReassembledPayloadLength)
	}
	return nil
}
