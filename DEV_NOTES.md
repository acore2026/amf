# Developer Notes

## Local Test Workflow

- Use `./scripts/test-local.sh` for routine AMF regression runs in this repo.
- The script sets a repo-local `GOCACHE` and `GOFLAGS=-buildvcs=false`, which avoids sandbox and environment-specific cache issues.
- Equivalent explicit command:

```bash
GOCACHE=/home/acore/proj/go/amf/.cache/gocache GOFLAGS=-buildvcs=false go test ./...
```

## Procedure-Level Coverage

- AMF has a local registration procedure test in [internal/ngap/registration_procedure_test.go](/home/acore/proj/go/amf/internal/ngap/registration_procedure_test.go).
- `TestInitialRegistrationProcedure` covers a full initial registration path using:
  - mock RAN
  - mock NRF, AUSF, UDM, and PCF via local `httptest` servers
- Focused command:

```bash
GOCACHE=/home/acore/proj/go/amf/.cache/gocache GOFLAGS=-buildvcs=false go test ./internal/ngap -run TestInitialRegistrationProcedure -count=1
```

## NAS Development Wiring

- During local development, AMF is pinned to the forked NAS repo with:

```go
replace github.com/free5gc/nas => ../nas
```

- Active NAS repo: `/home/acore/proj/go/nas`
- Relevant Release 17 message integration is in:
  - `/home/acore/proj/go/nas/nasMessage/NAS_ConfigurationUpdateCommand.go`

## Release 17 Registration Wait Range

- `CONFIGURATION UPDATE COMMAND` support was added in the NAS fork for:
  - `0x14` `Disaster roaming wait range`
  - `0x2C` `Disaster return wait range`
- Both use the shared `Registration wait range` format from TS 24.501 section `9.11.3.84`.
