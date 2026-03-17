## 1. Processor Implementation

- [x] 1.1 Add `ProvidePositioningInfoProcedure` to `internal/sbi/processor/location_info.go` to handle the business logic of retrieving positioning data.
- [x] 1.2 Implement UE context lookup and error handling (404 NOT_FOUND) within `ProvidePositioningInfoProcedure`.
- [x] 1.3 Implement the extraction of location information from the `AmfUe` context and populate the `models.ProvidePosInfo` structure.
- [x] 1.4 Add `HandleProvidePositioningInfoRequest` to `internal/sbi/processor/location_info.go` to wrap the procedure and handle the HTTP response.

## 2. SBI API Implementation

- [x] 2.1 Modify `HTTPProvidePositioningInfo` in `internal/sbi/api_location.go` to deserialize the incoming `models.RequestPosInfo` request body.
- [x] 2.2 Update the `HTTPProvidePositioningInfo` handler to invoke the newly created processor method.
- [x] 2.3 Ensure the handler correctly sets the response status and returns the JSON positioning information.

## 3. Verification

- [x] 3.1 Create a new test or add a test case to verify `ProvidePositioningInfoProcedure` with both successful and error scenarios.
- [x] 3.2 Manually verify the API response by sending a mock request (if environment allows).
