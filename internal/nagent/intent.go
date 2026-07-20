package nagent

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Intent struct {
	IntentID          string `json:"intentId"`
	Issuer            string `json:"issuer"`
	IntentPriority    int    `json:"intentPriority"`
	IntentType        string `json:"intentType"`
	IntentDescription string `json:"intentDescription"`
	Object            string `json:"object"`
	Constraint        string `json:"constraint"`
	Target            string `json:"target"`
}

type intentWire struct {
	IntentID          *string `json:"intentId"`
	Issuer            *string `json:"issuer"`
	IntentPriority    *int    `json:"intentPriority"`
	IntentType        *string `json:"intentType"`
	IntentDescription *string `json:"intentDescription"`
	Object            *string `json:"object"`
	Constraint        *string `json:"constraint"`
	Target            *string `json:"target"`
}

func ValidateIntentRequestBody(payload []byte) (Intent, []byte, error) {
	if !json.Valid(payload) {
		return Intent{}, nil, &Error{Code: ErrorCodeInvalidJSON}
	}

	var wire intentWire
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Intent{}, nil, &Error{
			Code:  ErrorCodeInvalidRequest,
			Cause: fmt.Errorf("decode Intent: %w", err),
		}
	}
	if wire.IntentID == nil || wire.Issuer == nil || wire.IntentPriority == nil ||
		wire.IntentType == nil || wire.IntentDescription == nil || wire.Object == nil ||
		wire.Constraint == nil || wire.Target == nil {
		return Intent{}, nil, &Error{
			Code:  ErrorCodeInvalidRequest,
			Cause: fmt.Errorf("Intent is missing one or more required fields"),
		}
	}

	intent := Intent{
		IntentID:          *wire.IntentID,
		Issuer:            *wire.Issuer,
		IntentPriority:    *wire.IntentPriority,
		IntentType:        *wire.IntentType,
		IntentDescription: *wire.IntentDescription,
		Object:            *wire.Object,
		Constraint:        *wire.Constraint,
		Target:            *wire.Target,
	}
	return intent, append([]byte(nil), payload...), nil
}
