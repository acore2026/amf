package processor_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/sbi/consumer"
	"github.com/acore2026/amf/internal/sbi/processor"
	"github.com/acore2026/amf/pkg/app"
	"github.com/acore2026/openapi/models"
)

type MockProcessorAmf struct {
	*app.MockApp
}

func (m *MockProcessorAmf) Consumer() *consumer.Consumer {
	return nil
}

func (m *MockProcessorAmf) Start() {}

func (m *MockProcessorAmf) Terminate() {}

func TestProvidePositioningInfoProcedure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockApp := app.NewMockApp(ctrl)
	mockProcessorAmf := &MockProcessorAmf{MockApp: mockApp}
	p, err := processor.NewProcessor(mockProcessorAmf)
	assert.NoError(t, err)

	amfContext := context.GetSelf()
	amfContext.Reset()
	amfContext.ServedGuamiList = append(amfContext.ServedGuamiList, models.Guami{
		PlmnId: &models.PlmnIdNid{
			Mcc: "208",
			Mnc: "93",
		},
		AmfId: "cafe00",
	})

	// Case 1: UE Context Not Found
	req := models.RequestPosInfo{}
	resp, problemDetails := p.ProvidePositioningInfoProcedure(req, "imsi-123456789")
	assert.Nil(t, resp)
	assert.NotNil(t, problemDetails)
	assert.Equal(t, int32(http.StatusNotFound), problemDetails.Status)
	assert.Equal(t, "CONTEXT_NOT_FOUND", problemDetails.Cause)

	// Case 2: UE Context Found but no AN Type (Access Type)
	ue := amfContext.NewAmfUe("imsi-123456789")
	resp, problemDetails = p.ProvidePositioningInfoProcedure(req, "imsi-123456789")
	assert.Nil(t, resp)
	assert.NotNil(t, problemDetails)
	assert.Equal(t, int32(http.StatusNotFound), problemDetails.Status)
	assert.Equal(t, "CONTEXT_NOT_FOUND", problemDetails.Cause)

	// Case 3: Success
	ue.RanUe[models.AccessType__3_GPP_ACCESS] = &context.RanUe{}
	ue.Location = models.UserLocation{
		NrLocation: &models.NrLocation{
			Tai: &models.Tai{
				PlmnId: &models.PlmnId{
					Mcc: "208",
					Mnc: "93",
				},
				Tac: "000001",
			},
			Ncgi: &models.Ncgi{
				PlmnId: &models.PlmnId{
					Mcc: "208",
					Mnc: "93",
				},
				NrCellId: "000000001",
			},
		},
	}

	resp, problemDetails = p.ProvidePositioningInfoProcedure(req, "imsi-123456789")
	assert.Nil(t, problemDetails)
	assert.NotNil(t, resp)
	assert.Equal(t, ue.Location.NrLocation.Ncgi, resp.Ncgi)
}
