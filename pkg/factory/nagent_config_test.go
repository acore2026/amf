package factory

import (
	"testing"

	"gopkg.in/yaml.v2"
)

func TestNAgentDefaults(t *testing.T) {
	cfg := &Config{Configuration: &Configuration{}}

	got := cfg.GetNAgentConfig()
	if got.Enabled {
		t.Fatal("NAgent should be disabled when configuration is absent")
	}
	if got.BaseURI != "http://127.0.0.1:8088" || got.ConnectTimeoutMs != 1000 ||
		got.AttemptTimeoutMs != 2000 || got.TotalTimeoutMs != 3000 || got.MaxAttempts != 3 ||
		got.MaxPayloadBytes != 65535 || got.MaxInFlight != 64 ||
		got.MaxInFlightPerUE != 8 || got.QueueSize != 256 || got.PendingDLTTLSeconds != 60 ||
		got.Mock.Enabled || got.Mock.ListenAddress != "127.0.0.1:8088" ||
		got.Mock.DelayMs != 0 || got.Mock.Status != 200 ||
		got.TransportPassthrough.Enabled || got.TransportPassthrough.PayloadContainerType != 4 ||
		got.TransportPassthrough.MaxPayloadBytes != 1400 {
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
			name: "valid embedded mock",
			config: NAgent{
				Enabled: true,
				Mock: NAgentMock{
					Enabled:       true,
					ListenAddress: "127.0.0.1:8088",
					DelayMs:       100,
					Status:        200,
				},
			},
		},
		{
			name: "invalid embedded mock address",
			config: NAgent{
				Enabled: true,
				Mock:    NAgentMock{Enabled: true, ListenAddress: "127.0.0.1"},
			},
			wantErr: true,
		},
		{
			name: "invalid embedded mock delay",
			config: NAgent{
				Enabled: true,
				Mock:    NAgentMock{Enabled: true, DelayMs: -1},
			},
			wantErr: true,
		},
		{
			name: "invalid embedded mock status",
			config: NAgent{
				Enabled: true,
				Mock:    NAgentMock{Enabled: true, Status: 700},
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
		{
			name: "valid NAS Transport passthrough",
			config: NAgent{
				Enabled: true,
				TransportPassthrough: NAgentTransportPassthrough{
					Enabled:              true,
					PayloadContainerType: 4,
					MaxPayloadBytes:      1400,
				},
			},
		},
		{
			name: "invalid NAS Transport passthrough payload container type",
			config: NAgent{
				Enabled: true,
				TransportPassthrough: NAgentTransportPassthrough{
					Enabled:              true,
					PayloadContainerType: 16,
					MaxPayloadBytes:      1400,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid NAS Transport passthrough max payload",
			config: NAgent{
				Enabled: true,
				TransportPassthrough: NAgentTransportPassthrough{
					Enabled:              true,
					PayloadContainerType: 4,
					MaxPayloadBytes:      65536,
				},
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

func TestNAgentEmbeddedMockYAML(t *testing.T) {
	var parsed struct {
		NAgent NAgent `yaml:"nagent"`
	}
	err := yaml.Unmarshal([]byte(`
nagent:
  enabled: true
  baseUri: http://127.0.0.1:19088
  mock:
    enabled: true
    listenAddress: 127.0.0.1:19088
    delayMs: 250
    status: 503
  transportPassthrough:
    enabled: true
    payloadContainerType: 4
    maxPayloadBytes: 1200
`), &parsed)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	got := parsed.NAgent.withDefaults()
	if !got.Enabled || !got.Mock.Enabled || got.BaseURI != "http://127.0.0.1:19088" ||
		got.Mock.ListenAddress != "127.0.0.1:19088" || got.Mock.DelayMs != 250 ||
		got.Mock.Status != 503 || !got.TransportPassthrough.Enabled ||
		got.TransportPassthrough.PayloadContainerType != 4 ||
		got.TransportPassthrough.MaxPayloadBytes != 1200 {
		t.Fatalf("decoded NAgent mock configuration = %#v", got)
	}
}
