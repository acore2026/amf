package nagent

import (
	"bytes"
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

func TestValidateIntentRequestBodyRejectsInvalidIntent(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		code    string
	}{
		{name: "invalid JSON", payload: []byte(`{"intentId":`), code: ErrorCodeInvalidJSON},
		{name: "missing field", payload: []byte(`{"intentId":"one"}`), code: ErrorCodeInvalidRequest},
		{
			name: "unknown field",
			payload: []byte(`{
                "intentId":"one","issuer":"ue","intentPriority":1,"intentType":"type",
                "intentDescription":"description","object":"object","constraint":"","target":"target",
                "unexpected":true
            }`),
			code: ErrorCodeInvalidRequest,
		},
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
