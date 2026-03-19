package nas

import (
	"errors"
	"fmt"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/gmm"
	"github.com/acore2026/amf/internal/logger"
	"github.com/acore2026/nas"
	"github.com/acore2026/openapi/models"
	"github.com/acore2026/util/fsm"
)

func Dispatch(ue *context.AmfUe, accessType models.AccessType, procedureCode int64, msg *nas.Message) error {
	if msg.GmmMessage == nil {
		return errors.New("gmm Message is nil")
	}

	if msg.GsmMessage != nil {
		return errors.New("GSM Message should include in GMM Message")
	}

	if ue.State[accessType] == nil {
		return fmt.Errorf("UE State is empty (accessType=%q). Can't send GSM Message", accessType)
	}

	return gmm.GmmFSM.SendEvent(ue.State[accessType], gmm.GmmMessageEvent, fsm.ArgsType{
		gmm.ArgAmfUe:         ue,
		gmm.ArgAccessType:    accessType,
		gmm.ArgNASMessage:    msg.GmmMessage,
		gmm.ArgProcedureCode: procedureCode,
	}, logger.GmmLog)
}
