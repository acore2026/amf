## Why

The `ProvidePositioningInfo` API in the `Namf_Location` service is currently unimplemented, returning a `501 Not Implemented` status. This change aims to provide a baseline implementation that returns the last known cell-ID for a UE, satisfying the API contract for basic location reporting requirements.

## What Changes

- Implement the `HTTPProvidePositioningInfo` handler in `internal/sbi/api_location.go` to process incoming positioning information requests.
- Add a new procedure `ProvidePositioningInfoProcedure` in `internal/sbi/processor/location_info.go` to handle the business logic of retrieving UE positioning data.
- Integrate with the internal `AmfUe` context to extract the last known user location (cell-ID).
- Ensure proper error handling, specifically for cases where the UE context cannot be found.

## Capabilities

### New Capabilities
- `namf-location-provide-positioning-info`: Implements the `ProvidePositioningInfo` service operation within the `Namf_Location` SBI, allowing consumers to request positioning information for a specific UE context.

### Modified Capabilities
<!-- No existing requirement-level specs to modify -->

## Impact

- **SBI Layer**: `internal/sbi/api_location.go` will be modified to include the handler implementation.
- **Processor Layer**: `internal/sbi/processor/location_info.go` will be extended with the new procedure.
- **Models**: Usage of `models.RequestPosInfo` and `models.ProvidePosInfo` from the `openapi` dependency.
