package nagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateIntentRequestBody(t *testing.T) {
	payload := []byte(`{
        "intentId":"intent-001",
        "issuer":"ue",
        "intentPriority":10,
        "intentType":"location",
        "intentDescription":"Locate the target UE",
        "object":"ue-location",
        "constraint":"accuracy<100m",
        "target":"imsi-001010000000002"
    }`)

	intent, body, err := ValidateIntentRequestBody(payload)
	if err != nil {
		t.Fatalf("ValidateIntentRequestBody() error = %v", err)
	}
	if intent.IntentID != "intent-001" || intent.IntentPriority != 10 ||
		intent.IntentDescription != "Locate the target UE" {
		t.Fatalf("decoded Intent = %#v", intent)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("HTTP body = %s, want original payload %s", body, payload)
	}
}

func TestValidateIntentRequestBodyAllowsExtraFields(t *testing.T) {
	payload := []byte(`{
        "intentId":"one","issuer":"ue","intentPriority":1,"intentType":"type",
        "intentDescription":"description","object":"object","constraint":"","target":"target",
        "request_id":"req-1","intent_type":"type","source_device":"imsi-001"
    }`)

	_, _, err := ValidateIntentRequestBody(payload)
	if err != nil {
		t.Fatalf("ValidateIntentRequestBody() with extra fields error = %v", err)
	}
}

func TestValidateIntentRequestBodyRejectsInvalidIntent(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		code    string
	}{
		{name: "invalid JSON", payload: []byte(`{"intentId":`), code: ErrorCodeInvalidJSON},
		{name: "missing field", payload: []byte(`{"intentId":"one"}`), code: ErrorCodeInvalidRequest},
		{
			name: "non-integer priority",
			payload: []byte(`{
                "intentId":"one","issuer":"ue","intentPriority":1.5,"intentType":"type",
                "intentDescription":"description","object":"object","constraint":"","target":"target"
            }`),
			code: ErrorCodeInvalidRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ValidateIntentRequestBody(test.payload)
			var intentError *Error
			if !errors.As(err, &intentError) || intentError.Code != test.code {
				t.Fatalf("error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestAdaptIntentPayloadAddsAgentFields(t *testing.T) {
	payload := []byte(`{"intentId":"intent-001","issuer":"ue","intentPriority":10,"intentType":"location","intentDescription":"desc","object":"obj","constraint":"c","target":"t"}`)
	supi := "imsi-001010000000001"

	adapted, err := AdaptIntentPayload(payload, supi)
	if err != nil {
		t.Fatalf("AdaptIntentPayload() error = %v", err)
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(adapted, &fields); err != nil {
		t.Fatalf("Adapted payload is not valid JSON: %v", err)
	}

	if fields["request_id"] != "intent-001" {
		t.Fatalf("request_id = %v, want intent-001", fields["request_id"])
	}
	if fields["intent_type"] != "location" {
		t.Fatalf("intent_type = %v, want location", fields["intent_type"])
	}
	if fields["source_device"] != supi {
		t.Fatalf("source_device = %v, want %s", fields["source_device"], supi)
	}
}

func TestAdaptIntentPayloadPreservesExistingAgentFields(t *testing.T) {
	payload := []byte(`{"intentId":"intent-001","issuer":"ue","intentPriority":10,"intentType":"location","intentDescription":"desc","object":"obj","constraint":"c","target":"t","request_id":"custom-req","intent_type":"custom-type","source_device":"custom-device"}`)
	supi := "imsi-001010000000001"

	adapted, err := AdaptIntentPayload(payload, supi)
	if err != nil {
		t.Fatalf("AdaptIntentPayload() error = %v", err)
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(adapted, &fields); err != nil {
		t.Fatalf("Adapted payload is not valid JSON: %v", err)
	}

	if fields["request_id"] != "custom-req" {
		t.Fatalf("request_id = %v, want custom-req (should not overwrite)", fields["request_id"])
	}
	if fields["intent_type"] != "custom-type" {
		t.Fatalf("intent_type = %v, want custom-type (should not overwrite)", fields["intent_type"])
	}
	if fields["source_device"] != "custom-device" {
		t.Fatalf("source_device = %v, want custom-device (should not overwrite)", fields["source_device"])
	}
}

func TestAdaptIntentPayloadRejectsInvalidJSON(t *testing.T) {
	_, err := AdaptIntentPayload([]byte(`{invalid`), "imsi-001")
	var intentError *Error
	if !errors.As(err, &intentError) || intentError.Code != ErrorCodeInvalidJSON {
		t.Fatalf("error = %v, want code %s", err, ErrorCodeInvalidJSON)
	}
}
