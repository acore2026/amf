# CLAUDE.md - free5gc-amf Project Context

## Project Overview

This is the **AMF (Access and Mobility Management Function)** from the free5gc open-source 5G core network project. It implements the 3GPP specifications for 5G mobility management, handling UE registration, authentication, and mobility.

- **Language**: Go (1.25.5+)
- **License**: Apache 2.0
- **Repository**: https://github.com/free5gc/amf

## Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                         AMF                                  │
├─────────────┬─────────────┬─────────────┬───────────────────┤
│     SBI     │    NGAP     │     NAS     │       GMM         │
│   (HTTP/2)  │   (SCTP)    │  (Security) │   (State Machine) │
├─────────────┴─────────────┴─────────────┴───────────────────┤
│                      Context Layer                           │
│         (UE Context, RAN Context, AMF Context)              │
└─────────────────────────────────────────────────────────────┘
```

| Component | Path | Purpose |
|-----------|------|---------|
| **SBI** | `internal/sbi/` | Service-Based Interface - HTTP/2 REST APIs for inter-NF communication |
| **NGAP** | `internal/ngap/` | NG Application Protocol - SCTP-based RAN communication |
| **NAS** | `internal/nas/` | Non-Access Stratum - UE signaling with encryption/integrity |
| **GMM** | `internal/gmm/` | 5G Mobility Management - FSM for registration/auth states |
| **Context** | `internal/context/` | UE, RAN, and AMF context management |

### Key Files

| File | Purpose |
|------|---------|
| `cmd/main.go` | Application entry point |
| `pkg/service/init.go` | AmfApp lifecycle (Start, Terminate) |
| `pkg/factory/config.go` | Configuration structures & validation |
| `internal/gmm/init.go` | GMM FSM state definitions |
| `internal/gmm/sm.go` | State machine handlers |
| `internal/ngap/dispatcher.go` | NGAP message routing |
| `internal/nas/dispatch.go` | NAS message routing to GMM |

## Configuration

The AMF uses YAML configuration (default: `./config/amfcfg.yaml`):

```yaml
configuration:
  amfName: AMF
  ngapIpList:
    - 127.0.0.1
  sbi:
    scheme: http
    bindingIPv4: 0.0.0.0
    port: 8000
  serviceNameList:
    - namf-comm
    - namf-evts
    - namf-mt
    - namf-loc
    - namf-oam
  nrfUri: http://127.0.0.1:8001
```

## GMM State Machine

The 5GMM (5G Mobility Management) FSM has these main states:

```
┌─────────────────┐
│  DeRegistered   │ ← Initial state
└────────┬────────┘
         │ Registration Request
         ▼
┌─────────────────┐     ┌─────────────────┐
│  Authentication │────→│  SecurityMode   │
└─────────────────┘     └────────┬────────┘
                                 │
         ┌───────────────────────┘
         ▼
┌─────────────────┐     ┌─────────────────┐
│ ContextSetup    │────→│   Registered    │ ← Normal operating state
└─────────────────┘     └─────────────────┘
                                 │
         ┌───────────────────────┘
         ▼
┌─────────────────┐
│DeregisteredInit │ ← Deregistration in progress
└─────────────────┘
```

## Inter-NF Communication

The AMF communicates with other 5G Network Functions:

| NF | Purpose | Client Location |
|----|---------|-----------------|
| **NRF** | NF Discovery & Registration | `internal/sbi/consumer/nrf.go` |
| **UDM** | User data retrieval | `internal/sbi/consumer/udm.go` |
| **AUSF** | Authentication | `internal/sbi/consumer/ausf.go` |
| **SMF** | Session Management | `internal/sbi/consumer/smf.go` |
| **PCF** | Policy control | `internal/sbi/consumer/pcf.go` |
| **NSSF** | Network slice selection | `internal/sbi/consumer/nssf.go` |

## Development Guidelines

### Adding a New State Handler

1. Define the state in `internal/gmm/init.go`
2. Add state entry/exit handlers in `internal/gmm/sm.go`
3. Register transitions in the FSM

### Adding NGAP Message Handling

1. Add case in `internal/ngap/dispatcher.go`
2. Implement handler in `internal/ngap/handler.go`
3. Use `internal/ngap/message/build.go` for responses

### Adding SBI API Endpoints

1. Add route in `internal/sbi/routes.go`
2. Implement handler in `internal/sbi/api_*.go`
3. Add processor logic in `internal/sbi/processor/`

### Running Tests

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/gmm/...
go test ./internal/ngap/...

# Run with coverage
go test -cover ./...
```

### Linting

```bash
golangci-lint run
```

## Common Patterns

### Context Access Pattern
```go
// Get AMF context
amfSelf := context.AMF_Self()

// Get UE by SUPI
ue, ok := amfSelf.AmfUeFindBySupi(supi)

// Get UE by GUTI
ue, ok := amfSelf.AmfUeFindByGuti(guti)
```

### Sending NGAP Message
```go
// Build and send NGAP message
ngap_message.SendDownlinkNasTransport(ranUe, nasPdu, nil)
```

### Sending GMM Message
```go
// Build and send GMM message
gmm_message.SendRegistrationAccept(ue, anType, pduSessionStatus)
```

## Important Notes

1. **Thread Safety**: UE contexts are protected by mutexes - always lock before modifying
2. **NAS Security**: NAS messages must be integrity protected; encryption is optional based on UE capabilities
3. **NGAP Scheduler**: Uses worker pool for concurrent message processing
4. **Timer Management**: GMM procedures use timers (T3513, T3522, etc.) defined in config
5. **State Consistency**: Always update UE state through GMM FSM, not directly

## Troubleshooting

### SCTP Connection Issues
- Check `ngapIpList` in config matches gNB configuration
- Verify SCTP kernel module is loaded: `lsmod | grep sctp`

### NRF Registration Failures
- Verify `nrfUri` is accessible
- Check NF profile in `pkg/factory/config.go` service names

### NAS Security Failures
- Verify security algorithms in config match UE capabilities
- Check that NAS sequence numbers are synchronized

## Related Documentation

- 3GPP TS 23.501: 5G System Architecture
- 3GPP TS 23.502: Procedures for 5G System
- 3GPP TS 24.501: NAS Protocol for 5GS
- 3GPP TS 38.413: NGAP Specification
- free5gc documentation: https://free5gc.org/
