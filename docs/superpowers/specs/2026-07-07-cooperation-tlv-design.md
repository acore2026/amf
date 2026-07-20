# Cooperation TLV Design

> Historical note: this was the initial TLV design. The current implementation keeps the independent IE list, but it does not use one-byte lengths for every Cooperation message. `MessageIdentity == 0x01` uses a one-byte IE length, while other identities keep a two-byte legacy outer IE length. Current AP Container and NAgent behavior is documented in `docs/ap-intent-nagent-flow.md` and `docs/cooperation-porting-guide.md`.

## Context

The current AMF implementation treats `ULCooperation` and `DLCooperation` as local NAS extensions with message types `0xe1` and `0xe2`. The existing code models the payload primarily as an AP container IE with IEI `0x71` and a two-byte length.

The original desired protocol was different: each information element after `MessageIdentity` is an independent TLV, multiple TLVs may appear in one Cooperation message, and all Cooperation IE lengths are one byte.

This design replaces the single-container payload model with a generic TLV model.

## Wire Format

Both UL and DL Cooperation messages use the same header layout:

```text
ExtendedProtocolDiscriminator        1 byte
SpareHalfOctetAndSecurityHeaderType  1 byte
MessageType                          1 byte
MessageIdentity                      1 byte
```

The historical one-byte-length body was zero or more independent TLVs:

```text
IEI                                  1 byte
Length                               1 byte
Value                                Length bytes
```

Example UL Cooperation capability negotiation message:

```text
7e 00 e1 01 10 01 01 18 01 01
```

This decodes as:

```text
EPD             = 0x7e
SecurityHeader  = 0x00
MessageType     = 0xe1
MessageIdentity = 0x01

IE 0x10: Len=1, Value=01
IE 0x18: Len=1, Value=01
```

Example with three UL IEs:

```text
7e 00 e1 01 10 01 01 18 01 01 71 02 aa bb
```

This decodes as independent IEs:

```text
IE 0x10: Len=1, Value=01
IE 0x18: Len=1, Value=01
IE 0x71: Len=2, Value=aa bb
```

## Known IEI Values

UL Cooperation supports these known IEIs:

```text
0x10
0x18
0x71
```

DL Cooperation supports these known IEIs:

```text
0x10
0x71
```

Unknown IEIs are decoded and retained in the parsed IE list. They are logged and ignored by default unless a handler is registered for them later.

## Data Model

Introduce a shared Cooperation IE type:

```go
type CooperationIE struct {
    Iei      uint8
    Len      uint8
    Contents []byte
}
```

`ULCooperation` and `DLCooperation` should both use a list of these IEs:

```go
type ULCooperation struct {
    nasType.ExtendedProtocolDiscriminator
    nasType.SpareHalfOctetAndSecurityHeaderType
    MessageType    uint8
    MessageIdentity uint8
    IEs            []*CooperationIE
}

type DLCooperation struct {
    nasType.ExtendedProtocolDiscriminator
    nasType.SpareHalfOctetAndSecurityHeaderType
    MessageType    uint8
    MessageIdentity uint8
    IEs            []*CooperationIE
}
```

Convenience methods should be provided:

```go
AddIE(iei uint8, contents []byte)
GetIE(iei uint8) *CooperationIE
GetIEs(iei uint8) []*CooperationIE
HasIE(iei uint8) bool
```

The parsed IE order should be preserved. This makes behavior predictable if future protocol versions allow repeated IEIs.

## Encoding And Decoding

Decoding should:

1. Read the 4-byte Cooperation header.
2. Loop until no bytes remain.
3. Require at least 2 bytes for `IEI` and `Length`.
4. Read `Length` bytes as `Value`.
5. Append the IE to the Cooperation message.

Decoding should fail if:

```text
remaining bytes < 2 before reading an IE
Length > remaining bytes after reading IEI and Length
```

Encoding should:

1. Write the 4-byte Cooperation header.
2. For each IE, write `IEI`, `Length`, and `Value`.
3. Validate that `Length == len(Value)`.
4. Reject values longer than 255 bytes.

Zero-length IEs are allowed if represented as:

```text
IEI 00
```

## GMM Handling

`HandleULCooperation` should no longer only inspect `ULApContainer`. It should dispatch each parsed IE independently.

Use an IE handler registry:

```go
type CooperationIEHandler func(ue *context.AmfUe, anType models.AccessType, ie *nasMessage.CooperationIE) ([]*nasMessage.CooperationIE, error)

var ulIEHandlers = map[uint8]CooperationIEHandler{
    0x10: handleULCooperationIE10,
    0x18: handleULCooperationIE18,
    0x71: handleULCooperationIE71,
}
```

Each UL IE handler may:

```text
validate the IE value
update AMF state
produce zero or more DL Cooperation IEs
return an error
```

The collected DL IEs are passed into `BuildDLCooperation`.

DL construction should validate that all emitted IEIs are allowed for DL. If a handler emits an unsupported DL IEI, the build should fail rather than silently sending an invalid response.

## UE Cooperation State

The current AMF implementation does not have a dedicated storage mechanism for Cooperation capability negotiation results. It logs `ULCooperation` contents and mirrors `ULApContainer` into `DLCooperation`, but it does not persist the negotiated IE values in `AmfUe`.

Add a small per-UE Cooperation context under `context.AmfUe`:

```go
type CooperationContext struct {
    LastMessageIdentity uint8
    LastULIEs           map[uint8][][]byte
    NegotiatedIEs       map[uint8][]byte
    UpdatedAt           time.Time
}
```

`LastULIEs` preserves the raw values received in the most recent UL Cooperation message, grouped by IEI. It supports repeated IEIs if a later version allows them.

`NegotiatedIEs` stores the current effective negotiated value for each IEI. Each IE handler decides whether and how to update it.

Suggested handler behavior:

```text
IE 0x10: validate and store negotiated capability value; may emit DL IE 0x10
IE 0x18: validate and store/report UL-only capability value; does not emit DL IE 0x18
IE 0x71: validate and store negotiated value; may emit DL IE 0x71
```

DL handlers should build responses from handler output and/or `CooperationContext`, not from implicit mirroring. This keeps capability negotiation explicit and testable.

## Compatibility

The old AP container layout is not part of the desired protocol:

```text
71 00 6e ...
```

The new desired layout is:

```text
71 02 aa bb
```

where `02` is a one-byte length.

The current implementation did keep the two-byte outer IE length for `MessageIdentity != 0x01`, gated explicitly by `MessageIdentity`. Both outer-length variants still carry the same new AP Container internal structure.

Recommended versioning policy:

```text
MessageIdentity = 0x01: new independent TLV format
```

No legacy format is enabled unless explicitly required.

## Security Behavior

The Cooperation exchange is expected after UE registration completes. The outer NAS message should be security protected. After NAS security decoding, the inner Cooperation payload follows the plain TLV format described above.

The existing `MacFailed` guard in `HandleULCooperation` should remain meaningful for post-registration messages: if integrity verification fails, the Cooperation message should not be processed.

## Tests

Add focused tests for:

```text
UL decode: 7e 00 e1 01 10 01 01
UL decode: 7e 00 e1 01 18 01 01
UL decode: 7e 00 e1 01 71 02 aa bb
UL decode: 7e 00 e1 01 10 01 01 18 01 01 71 02 aa bb
DL encode: 7e 00 e2 01 10 01 00
DL encode: 7e 00 e2 01 10 01 01
DL encode: 7e 00 e2 01 71 02 aa bb
DL encode: 7e 00 e2 01 10 01 01 71 02 aa bb
Unknown IE decode and retention
Malformed IE missing Length
Malformed IE with Length greater than remaining bytes
Value longer than 255 bytes rejected on encode
UL handler stores latest raw IE values in CooperationContext
UL handler updates negotiated IE values in CooperationContext
UL IE 0x18 does not produce unsupported DL IE 0x18
```

Existing tests that assume `0x71` uses a two-byte length should be updated or removed if they represent the old protocol.

Root-level manual programs with `package main` should be moved under a command or test fixture path, or converted to `_test.go`, so they do not break `go test .`.
