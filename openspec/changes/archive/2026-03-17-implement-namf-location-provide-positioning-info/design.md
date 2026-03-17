## Context

The `Namf_Location` service is responsible for providing UE location and positioning information to consumers like the GMLC. Currently, the `ProvidePositioningInfo` operation is not implemented. The AMF stores the last known user location in the `AmfUe` context, which is updated via NGAP messages from the RAN.

## Goals / Non-Goals

**Goals:**
- Provide a functional implementation of the `ProvidePositioningInfo` service operation.
- Return the last known cell-ID (E-UTRA or NR location) for the specified UE.
- Maintain consistency with existing `Namf_Location` implementations in the codebase.

**Non-Goals:**
- Implementing actual positioning procedures (e.g., triggering the LMF via `Nlmf_Location`).
- Implementing `NRPPa` or `LPP` message transport for active location acquisition.
- Implementing the `CancelLocation` API.

## Decisions

### 1. Business Logic Placement
The business logic will be implemented as a new procedure `ProvidePositioningInfoProcedure` within `internal/sbi/processor/location_info.go`. 
- **Rationale**: This follows the existing architectural pattern where the SBI handler (`internal/sbi/api_location.go`) focuses on request/response serialization, while the `Processor` handles context retrieval and data extraction.

### 2. Response Content
The response will include the `Location` (from `ue.Location`) and set `CurrentLoc` to `true` (indicating the reported location is the current known location).
- **Rationale**: Since we are not triggering a new positioning procedure, we report the most recent location information available in the AMF context.

### 3. Error Handling
If the `ueContextId` is not found, the system will return `404 Not Found` with the `CONTEXT_NOT_FOUND` cause.
- **Rationale**: This is the standard 3GPP error response for missing UE contexts in AMF APIs.

## Risks / Trade-offs

- **[Risk] Accuracy of Location Data** → The reported location might be stale if the UE has moved since the last NGAP activity. 
  - **Mitigation**: This implementation is explicitly scoped as a "simple" stage to satisfy the API contract. Future iterations can add active positioning triggers.
- **[Risk] Model Compatibility** → The `RequestPosInfo` and `ProvidePosInfo` models in the `openapi` library must be compatible with the current Go version.
  - **Mitigation**: The user has confirmed the existence of these models in the specified library version.
