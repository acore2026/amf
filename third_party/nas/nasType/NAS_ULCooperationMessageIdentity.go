package nasType

// ULCooperationMessageIdentity 9.7
type ULCooperationMessageIdentity struct {
	Octet uint8
}

func NewULCooperationMessageIdentity() (uLCooperationMessageIdentity *ULCooperationMessageIdentity) {
	uLCooperationMessageIdentity = &ULCooperationMessageIdentity{}
	return uLCooperationMessageIdentity
}

func (a *ULCooperationMessageIdentity) GetMessageType() (messageType uint8) {
	return a.Octet
}

func (a *ULCooperationMessageIdentity) SetMessageType(messageType uint8) {
	a.Octet = messageType
}
