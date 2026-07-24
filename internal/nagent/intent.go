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

type AgentRoute struct {
	Name        string
	BaseURI     string
	Path        string
	Schema      string
	IntentTypes []string
}

func ValidateIntentRequestBody(payload []byte) (Intent, []byte, error) {
	if !json.Valid(payload) {
		return Intent{}, nil, &Error{Code: ErrorCodeInvalidJSON}
	}

	var wire intentWire
	decoder := json.NewDecoder(bytes.NewReader(payload))
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

func AdaptIntentPayload(payload []byte, supi string, routes []AgentRoute) ([]byte, *AgentRoute, error) {
	var fields map[string]interface{}
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, nil, &Error{Code: ErrorCodeInvalidJSON, Cause: err}
	}

	intentType, _ := fields["intentType"].(string)
	route := matchRoute(routes, intentType)
	if route == nil {
		route = &AgentRoute{Schema: "intent"}
	}

	switch route.Schema {
	case "voice":
		adaptVoiceSchema(fields, supi)
	default:
		adaptIntentSchema(fields, supi)
	}

	adapted, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, &Error{Code: ErrorCodeInvalidRequest, Cause: err}
	}
	return adapted, route, nil
}

func matchRoute(routes []AgentRoute, intentType string) *AgentRoute {
	for i := range routes {
		for _, t := range routes[i].IntentTypes {
			if t == intentType || t == "*" {
				return &routes[i]
			}
		}
	}
	return nil
}

func adaptIntentSchema(fields map[string]interface{}, supi string) {
	if _, ok := fields["request_id"]; !ok {
		if intentID, ok := fields["intentId"].(string); ok && intentID != "" {
			fields["request_id"] = intentID
		}
	}
	if _, ok := fields["intent_type"]; !ok {
		if intentType, ok := fields["intentType"].(string); ok {
			fields["intent_type"] = intentType
		}
	}
	if _, ok := fields["source_device"]; !ok {
		fields["source_device"] = map[string]string{
			"device_id":   supi,
			"device_type": "UE",
		}
	}
	if _, ok := fields["intent_payload"]; !ok {
		if _, ok := fields["intent"]; !ok {
			if desc, ok := fields["intentDescription"].(string); ok {
				fields["intent_payload"] = desc
			}
		}
	}
}

func adaptVoiceSchema(fields map[string]interface{}, supi string) {
	if _, ok := fields["request_id"]; !ok {
		if intentID, ok := fields["intentId"].(string); ok && intentID != "" {
			fields["request_id"] = intentID
		}
	}
	if _, ok := fields["action"]; !ok {
		if intentType, ok := fields["intentType"].(string); ok {
			fields["action"] = intentType
		}
	}
	if _, ok := fields["intent_payload"]; !ok {
		if desc, ok := fields["intentDescription"].(string); ok {
			fields["intent_payload"] = desc
		}
	}
	if _, ok := fields["ui_language"]; !ok {
		fields["ui_language"] = "zh"
	}
}
