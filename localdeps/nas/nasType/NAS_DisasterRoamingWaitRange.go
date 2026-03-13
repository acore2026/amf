package nasType

import "fmt"

const disasterRoamingWaitRangeContentsLen uint8 = 2

// DisasterRoamingWaitRange 9.11.3.84
// This IE uses the Registration wait range Type 4 format and carries the
// disaster roaming wait range when used with IEI 0x2C.
// MinimumRegistrationWaitTime Row, sBit, len = [0, 0], 8, 8
// MaximumRegistrationWaitTime Row, sBit, len = [1, 1], 8, 8
type DisasterRoamingWaitRange struct {
	Iei   uint8
	Len   uint8
	Octet [2]uint8
}

func NewDisasterRoamingWaitRange(iei uint8) *DisasterRoamingWaitRange {
	disasterRoamingWaitRange := &DisasterRoamingWaitRange{}
	disasterRoamingWaitRange.SetIei(iei)
	disasterRoamingWaitRange.SetLen(disasterRoamingWaitRangeContentsLen)
	return disasterRoamingWaitRange
}

// DisasterRoamingWaitRange 9.11.3.84
// Iei Row, sBit, len = [], 8, 8
func (a *DisasterRoamingWaitRange) GetIei() uint8 {
	return a.Iei
}

// DisasterRoamingWaitRange 9.11.3.84
// Iei Row, sBit, len = [], 8, 8
func (a *DisasterRoamingWaitRange) SetIei(iei uint8) {
	a.Iei = iei
}

// DisasterRoamingWaitRange 9.11.3.84
// Len Row, sBit, len = [], 8, 8
func (a *DisasterRoamingWaitRange) GetLen() uint8 {
	return a.Len
}

// DisasterRoamingWaitRange 9.11.3.84
// Len Row, sBit, len = [], 8, 8
func (a *DisasterRoamingWaitRange) SetLen(length uint8) {
	a.Len = length
}

// DisasterRoamingWaitRange 9.11.3.84
// MinimumRegistrationWaitTime Row, sBit, len = [0, 0], 8, 8
func (a *DisasterRoamingWaitRange) GetMinimumRegistrationWaitTime() uint8 {
	return a.Octet[0]
}

// DisasterRoamingWaitRange 9.11.3.84
// MinimumRegistrationWaitTime Row, sBit, len = [0, 0], 8, 8
func (a *DisasterRoamingWaitRange) SetMinimumRegistrationWaitTime(value uint8) {
	a.Octet[0] = value
}

// DisasterRoamingWaitRange 9.11.3.84
// MaximumRegistrationWaitTime Row, sBit, len = [1, 1], 8, 8
func (a *DisasterRoamingWaitRange) GetMaximumRegistrationWaitTime() uint8 {
	return a.Octet[1]
}

// DisasterRoamingWaitRange 9.11.3.84
// MaximumRegistrationWaitTime Row, sBit, len = [1, 1], 8, 8
func (a *DisasterRoamingWaitRange) SetMaximumRegistrationWaitTime(value uint8) {
	a.Octet[1] = value
}

// Encode serializes the IE as a Type 4 TLV: IEI, length, minimum wait time,
// maximum wait time.
func (a *DisasterRoamingWaitRange) Encode() ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("disaster roaming wait range is nil")
	}
	if a.Len != disasterRoamingWaitRangeContentsLen {
		return nil, fmt.Errorf("invalid disaster roaming wait range length: %d", a.Len)
	}

	return []byte{a.Iei, a.Len, a.Octet[0], a.Octet[1]}, nil
}

// Decode parses the raw TLV bytes into the IE fields.
func (a *DisasterRoamingWaitRange) Decode(raw []byte) error {
	if a == nil {
		return fmt.Errorf("disaster roaming wait range is nil")
	}
	if len(raw) != 4 {
		return fmt.Errorf("invalid disaster roaming wait range raw length: %d", len(raw))
	}
	if raw[1] != disasterRoamingWaitRangeContentsLen {
		return fmt.Errorf("invalid disaster roaming wait range contents length: %d", raw[1])
	}

	a.Iei = raw[0]
	a.Len = raw[1]
	a.Octet[0] = raw[2]
	a.Octet[1] = raw[3]

	return nil
}
