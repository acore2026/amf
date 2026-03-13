package nasType_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/free5gc/nas/nasType"
)

const disasterRoamingWaitRangeIEI uint8 = 0x2C

var validDisasterRoamingWaitRangeRaw = []byte{
	disasterRoamingWaitRangeIEI,
	0x02,
	0x25,
	0x4A,
}

func TestDisasterRoamingWaitRangeDecode(t *testing.T) {
	ie := nasType.NewDisasterRoamingWaitRange(0x00)

	err := ie.Decode(validDisasterRoamingWaitRangeRaw)
	require.NoError(t, err)

	assert.Equal(t, disasterRoamingWaitRangeIEI, ie.GetIei())
	assert.Equal(t, uint8(0x02), ie.GetLen())
	assert.Equal(t, uint8(0x25), ie.GetMinimumRegistrationWaitTime())
	assert.Equal(t, uint8(0x4A), ie.GetMaximumRegistrationWaitTime())
}

func TestDisasterRoamingWaitRangeEncode(t *testing.T) {
	ie := nasType.NewDisasterRoamingWaitRange(disasterRoamingWaitRangeIEI)
	ie.SetMinimumRegistrationWaitTime(0x25)
	ie.SetMaximumRegistrationWaitTime(0x4A)

	encoded, err := ie.Encode()
	require.NoError(t, err)

	assert.Equal(t, validDisasterRoamingWaitRangeRaw, encoded)
}

func TestDisasterRoamingWaitRangeRoundTrip(t *testing.T) {
	decoded := nasType.NewDisasterRoamingWaitRange(0x00)
	require.NoError(t, decoded.Decode(validDisasterRoamingWaitRangeRaw))

	encoded, err := decoded.Encode()
	require.NoError(t, err)

	assert.Equal(t, validDisasterRoamingWaitRangeRaw, encoded)
}

func TestDisasterRoamingWaitRangeDecodeRejectsInvalidLength(t *testing.T) {
	ie := nasType.NewDisasterRoamingWaitRange(0x00)

	err := ie.Decode([]byte{disasterRoamingWaitRangeIEI, 0x01, 0x25})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raw length")
}

func TestDisasterRoamingWaitRangeEncodeRejectsInvalidContentsLength(t *testing.T) {
	ie := nasType.NewDisasterRoamingWaitRange(disasterRoamingWaitRangeIEI)
	ie.SetLen(0x01)
	ie.SetMinimumRegistrationWaitTime(0x25)
	ie.SetMaximumRegistrationWaitTime(0x4A)

	_, err := ie.Encode()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid disaster roaming wait range length")
}
