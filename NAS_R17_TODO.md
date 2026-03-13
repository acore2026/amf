# Release 17 NAS TODO

## Status

- [x] Create `nasType.DisasterRoamingWaitRange`
- [x] Add `nasType.DisasterReturnWaitRange` using the same registration wait range format
- [x] Add raw IE encode/decode tests for `DisasterRoamingWaitRange`
- [x] Add raw IE encode/decode tests for `DisasterReturnWaitRange`
- [x] Move the NAS IE implementation into the forked `nas` repository
- [x] Point AMF to the local `../nas` fork with a temporary `replace`
- [x] Add `DisasterRoamingWaitRange` support to `nasMessage.ConfigurationUpdateCommand`
- [x] Add `DisasterReturnWaitRange` support to `nasMessage.ConfigurationUpdateCommand`
- [x] Add message-level encode/decode tests for `ConfigurationUpdateCommand` carrying the new IE
- [x] Run `go test ./nasType ./nasMessage` in the `nas` repository
- [x] Run AMF regression tests against the local `../nas` fork after message integration
- [x] Commit and push the NAS message-layer change

## Working Notes

- Active NAS repo: `/home/acore/proj/go/nas`
- Active AMF repo: `/home/acore/proj/go/amf`
- Current development wiring: `replace github.com/free5gc/nas => ../nas`
- Relevant NAS message file: `/home/acore/proj/go/nas/nasMessage/NAS_ConfigurationUpdateCommand.go`
- Spec-correct IEIs in `CONFIGURATION UPDATE COMMAND`:
  - `0x14` = Disaster roaming wait range
  - `0x2C` = Disaster return wait range
