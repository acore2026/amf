package sbi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	amf_context "github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/sbi/consumer"
	"github.com/acore2026/amf/internal/sbi/processor"
	"github.com/acore2026/amf/pkg/factory"
	"github.com/acore2026/openapi/models"
)

type testAMFApp struct {
	processor *processor.Processor
}

func (a *testAMFApp) SetLogEnable(bool)                {}
func (a *testAMFApp) SetLogLevel(string)               {}
func (a *testAMFApp) SetReportCaller(bool)             {}
func (a *testAMFApp) Start()                           {}
func (a *testAMFApp) Terminate()                       {}
func (a *testAMFApp) Context() *amf_context.AMFContext { return amf_context.GetSelf() }
func (a *testAMFApp) Config() *factory.Config          { return nil }
func (a *testAMFApp) Consumer() *consumer.Consumer     { return nil }
func (a *testAMFApp) Processor() *processor.Processor {
	return a.processor
}

func newTestMTServer(t *testing.T) *Server {
	t.Helper()

	app := &testAMFApp{}
	p, err := processor.NewProcessor(app)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	app.processor = p

	return &Server{ServerAmf: app}
}

func prepareMTTestContext(t *testing.T) {
	t.Helper()

	amfSelf := amf_context.GetSelf()
	oldServedGuamiList := amfSelf.ServedGuamiList
	oldT3513Cfg := amfSelf.T3513Cfg

	amfSelf.ServedGuamiList = []models.Guami{{
		PlmnId: &models.PlmnIdNid{
			Mcc: "001",
			Mnc: "01",
		},
		AmfId: "cafe00",
	}}
	amfSelf.T3513Cfg = factory.TimerValue{}

	t.Cleanup(func() {
		amfSelf.ServedGuamiList = oldServedGuamiList
		amfSelf.T3513Cfg = oldT3513Cfg
	})
}

func TestHTTPEnableUeReachabilityConnectedUE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prepareMTTestContext(t)

	server := newTestMTServer(t)
	amfSelf := amf_context.GetSelf()
	supi := "imsi-001010123456789"
	ue := amfSelf.NewAmfUe(supi)
	t.Cleanup(func() {
		ue.Remove()
	})

	ue.State[models.AccessType__3_GPP_ACCESS].Set(amf_context.Registered)
	ue.RanUe[models.AccessType__3_GPP_ACCESS] = &amf_context.RanUe{}

	body := []byte(`{"reachability":"REACHABLE","supportedFeatures":"1"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodPut, "/ue-contexts/"+supi+"/ue-reachind", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "ueContextId", Value: supi}}

	server.HTTPEnableUeReachability(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	expectedBody := `{"reachability":"REACHABLE","supportedFeatures":"1"}`
	if got := w.Body.String(); got != expectedBody {
		t.Fatalf("response body = %s, want %s", got, expectedBody)
	}
}

func TestHTTPEnableUeReachabilityPagesIdleRegisteredUE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prepareMTTestContext(t)

	server := newTestMTServer(t)
	amfSelf := amf_context.GetSelf()
	supi := "imsi-001010123456780"
	ue := amfSelf.NewAmfUe(supi)
	t.Cleanup(func() {
		ue.Remove()
	})

	ue.State[models.AccessType__3_GPP_ACCESS].Set(amf_context.Registered)
	ue.RegistrationArea[models.AccessType__3_GPP_ACCESS] = []models.Tai{{
		PlmnId: &models.PlmnId{
			Mcc: "001",
			Mnc: "01",
		},
		Tac: "000001",
	}}

	body := []byte(`{"reachability":"REACHABLE"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodPut, "/ue-contexts/"+supi+"/ue-reachind", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "ueContextId", Value: supi}}

	server.HTTPEnableUeReachability(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	if got := ue.OnGoing(models.AccessType__3_GPP_ACCESS).Procedure; got != amf_context.OnGoingProcedurePaging {
		t.Fatalf("on-going procedure = %s, want %s", got, amf_context.OnGoingProcedurePaging)
	}
}

func TestHTTPEnableUeReachabilityContextNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := newTestMTServer(t)

	body := []byte(`{"reachability":"REACHABLE"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodPut, "/ue-contexts/imsi-404/ue-reachind", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "ueContextId", Value: "imsi-404"}}

	server.HTTPEnableUeReachability(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusNotFound)
	}
}
