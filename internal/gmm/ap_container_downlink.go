package gmm

import (
	"fmt"
	"math"

	"github.com/acore2026/nas/nasMessage"
)

func buildDLAPContainerIEs(
	messageIdentity uint8,
	complete *nasMessage.APContainer,
) ([]*nasMessage.CooperationIE, error) {
	if complete == nil {
		return nil, fmt.Errorf("complete AP Container is nil")
	}
	if complete.DontFragment() {
		fragment := cloneCompleteAPContainer(complete)
		fragment.ContainerFlags = nasMessage.APContainerFlagDF
		fragment.FragmentOffset = 0
		return buildSingleDLAPContainerIE(messageIdentity, fragment)
	}
	if len(complete.Payload) == 0 {
		fragment := cloneCompleteAPContainer(complete)
		fragment.ContainerFlags = 0
		fragment.FragmentOffset = 0
		return buildSingleDLAPContainerIE(messageIdentity, fragment)
	}

	maxFragmentSize := dlAPContainerMaxFragmentSize(messageIdentity)
	result := make([]*nasMessage.CooperationIE, 0, (len(complete.Payload)+maxFragmentSize-1)/maxFragmentSize)
	for offset := 0; offset < len(complete.Payload); offset += maxFragmentSize {
		end := offset + maxFragmentSize
		if end > len(complete.Payload) {
			end = len(complete.Payload)
		}
		flags := uint8(0)
		if end < len(complete.Payload) {
			flags |= nasMessage.APContainerFlagMF
		}
		fragment := &nasMessage.APContainer{
			ContainerType:      complete.ContainerType,
			ContainerTypePTI:   complete.ContainerTypePTI,
			ContainerPayloadID: complete.ContainerPayloadID,
			ContainerFlags:     flags,
			FragmentOffset:     uint16(offset),
			Payload:            append([]byte(nil), complete.Payload[offset:end]...),
		}
		ie, err := encodeDLAPContainerIE(messageIdentity, fragment)
		if err != nil {
			return nil, err
		}
		result = append(result, ie)
	}
	return result, nil
}

func buildSingleDLAPContainerIE(
	messageIdentity uint8,
	container *nasMessage.APContainer,
) ([]*nasMessage.CooperationIE, error) {
	ie, err := encodeDLAPContainerIE(messageIdentity, container)
	if err != nil {
		return nil, err
	}
	return []*nasMessage.CooperationIE{ie}, nil
}

func encodeDLAPContainerIE(
	messageIdentity uint8,
	container *nasMessage.APContainer,
) (*nasMessage.CooperationIE, error) {
	contents, err := container.Encode()
	if err != nil {
		return nil, err
	}
	if messageIdentity == 0x01 {
		return nasMessage.NewCooperationIE(nasMessage.CooperationIEType71, contents)
	}
	if len(contents) > math.MaxUint16 {
		return nil, fmt.Errorf("legacy AP Container length %d exceeds 65535", len(contents))
	}
	return nasMessage.NewCooperationIELegacy(nasMessage.CooperationIEType71, contents), nil
}

func dlAPContainerMaxFragmentSize(messageIdentity uint8) int {
	if messageIdentity == 0x01 {
		return nasMessage.APContainerMaxOneByteDLFragmentSize
	}
	return nasMessage.APContainerMaxDLFragmentSize
}

func cloneCompleteAPContainer(in *nasMessage.APContainer) *nasMessage.APContainer {
	out := *in
	out.Payload = append([]byte(nil), in.Payload...)
	return &out
}
