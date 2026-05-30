package nasType

// DisasterReturnWaitRange 9.11.3.84
// Shares the same Registration wait range wire format as
// DisasterRoamingWaitRange and is distinguished by its IEI at the message
// layer.
type DisasterReturnWaitRange = DisasterRoamingWaitRange

func NewDisasterReturnWaitRange(iei uint8) *DisasterReturnWaitRange {
	disasterReturnWaitRange := &DisasterReturnWaitRange{}
	disasterReturnWaitRange.SetIei(iei)
	disasterReturnWaitRange.SetLen(registrationWaitRangeContentsLen)
	return disasterReturnWaitRange
}
