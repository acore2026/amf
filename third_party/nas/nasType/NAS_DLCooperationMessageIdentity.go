package nasType

type DLCooperationMessageIdentity struct {
	Octet uint8
}

func NewDLCooperationMessageIdentity() (dLCooperationMessageIdentity *DLCooperationMessageIdentity) {
	dLCooperationMessageIdentity = &DLCooperationMessageIdentity{}
	return dLCooperationMessageIdentity
}

func (a *DLCooperationMessageIdentity) GetMessageType() (messageType uint8) {
	return a.Octet
}

func (a *DLCooperationMessageIdentity) SetMessageType(messageType uint8) {
	a.Octet = messageType
}