## ADDED Requirements

### Requirement: Support for ProvidePositioningInfo API
The AMF SHALL implement the `ProvidePositioningInfo` service operation within the `Namf_Location` service based on 3GPP TS 29.518.

#### Scenario: Successful positioning info retrieval
- **WHEN** a valid POST request is received at `/:ueContextId/provide-pos-info` for an existing UE context.
- **THEN** the system returns a `200 OK` status with the positioning information containing the UE's last known location.

### Requirement: UE Context Retrieval
The system SHALL attempt to locate the internal `AmfUe` context using the `ueContextId` provided in the API request URI.

#### Scenario: UE context found
- **WHEN** the `ueContextId` matches an existing UE in the AMF's internal storage.
- **THEN** the positioning procedure continues using that UE's data.

### Requirement: Location Reporting
The system SHALL populate the positioning response with the last known user location data (cell-ID) stored in the retrieved `AmfUe` context.

#### Scenario: Populate location data
- **WHEN** the UE context is available.
- **THEN** the `ProvidePosInfo` response contains the UE's current location (e.g., `nrLocation` or `eutraLocation`).

### Requirement: Error Handling (UE Not Found)
The system SHALL return a `404 Not Found` response when the requested `ueContextId` does not correspond to any active UE context.

#### Scenario: Handle missing UE context
- **WHEN** the `ueContextId` does not match any existing UE.
- **THEN** the system returns a `404 Not Found` status with the cause `CONTEXT_NOT_FOUND`.
