package factory

import "testing"

func TestNAgentDefaults(t *testing.T) {
	cfg := &Config{Configuration: &Configuration{}}

	got := cfg.GetNAgentConfig()
	if got.Enabled {
		t.Fatal("NAgent should be disabled when configuration is absent")
	}
	if got.BaseURI != "http://127.0.0.1:8088" || got.ConnectTimeoutMs != 1000 ||
		got.AttemptTimeoutMs != 2000 || got.TotalTimeoutMs != 5000 || got.MaxAttempts != 3 ||
		got.MaxPayloadBytes != 65535 || got.MaxInFlight != 64 ||
		got.MaxInFlightPerUE != 8 || got.QueueSize != 256 || got.PendingDLTTLSeconds != 60 {
		t.Fatalf("unexpected NAgent defaults: %#v", got)
	}
}

func TestNAgentValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  NAgent
		wantErr bool
	}{
		{
			name: "disabled accepts empty settings",
			config: NAgent{
				Enabled: false,
			},
		},
		{
			name: "valid HTTP configuration",
			config: NAgent{
				Enabled:             true,
				BaseURI:             "http://nagent-mock:8080",
				ConnectTimeoutMs:    1000,
				AttemptTimeoutMs:    2000,
				TotalTimeoutMs:      5000,
				MaxAttempts:         3,
				MaxPayloadBytes:     65535,
				MaxInFlight:         64,
				MaxInFlightPerUE:    8,
				QueueSize:           256,
				PendingDLTTLSeconds: 60,
			},
		},
		{
			name: "HTTPS is outside first version",
			config: NAgent{
				Enabled: true,
				BaseURI: "https://nagent.example.com",
			},
			wantErr: true,
		},
		{
			name: "missing host",
			config: NAgent{
				Enabled: true,
				BaseURI: "http:///missing",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
