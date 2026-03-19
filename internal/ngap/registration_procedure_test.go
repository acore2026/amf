package ngap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	amf_context "github.com/acore/amf/internal/context"
	amf_nas "github.com/acore/amf/internal/nas"
	amf_nas_security "github.com/acore/amf/internal/nas/nas_security"
	nastesting "github.com/acore/amf/internal/nas/testing"
	ngaptesting "github.com/acore/amf/internal/ngap/testing"
	"github.com/acore/amf/internal/sbi/consumer"
	"github.com/acore/amf/pkg/factory"
	"github.com/free5gc/nas"
	"github.com/free5gc/nas/nasMessage"
	"github.com/free5gc/nas/nasType"
	nassecurity "github.com/free5gc/nas/security"
	libngap "github.com/free5gc/ngap"
	"github.com/free5gc/ngap/ngapType"
	"github.com/free5gc/openapi/models"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestInitialRegistrationProcedure(t *testing.T) {
	amfSelf := amf_context.GetSelf()
	amfSelf.Reset()
	t.Cleanup(amfSelf.Reset)

	cfg := newTestConfig()
	factory.AmfConfig = cfg
	NewAmfContext(amfSelf)
	amfSelf.NfId = uuid.NewString()

	servers := newRegistrationMockServers(t)
	defer servers.close()
	amfSelf.NrfUri = servers.nrf.URL

	_, err := consumer.NewConsumer(&testConsumerApp{cfg: cfg, ctx: amfSelf})
	require.NoError(t, err)

	connStub := new(ngaptesting.SctpConnStub)
	ran := NewAmfRan(connStub)
	ran.AnType = models.AccessType__3_GPP_ACCESS

	ranUe, err := ran.NewRanUe(1)
	require.NoError(t, err)
	ranUe.Tai = models.Tai{
		PlmnId: &models.PlmnId{Mcc: "208", Mnc: "93"},
		Tac:    "000001",
	}

	registrationRequest := nastesting.GetRegistrationRequest(
		nasMessage.RegistrationType5GSInitialRegistration,
		testSuciMobileIdentity(),
		nil,
		testUESecurityCapability(),
		testCapability5GMM(),
		nil,
		nil,
	)
	amf_nas.HandleNAS(ranUe, ngapType.ProcedureCodeInitialUEMessage, registrationRequest, true)
	amfUe := ranUe.AmfUe
	require.NotNil(t, amfUe)

	authRequestRaw := lastConnMessage(t, connStub)
	authRequestPdu, authRequestNas := decodeNgapNas(t, authRequestRaw, nil, models.AccessType__3_GPP_ACCESS)
	require.Equal(t, int64(ngapType.ProcedureCodeDownlinkNASTransport), authRequestPdu.InitiatingMessage.ProcedureCode.Value)
	require.Equal(t, nas.MsgTypeAuthenticationRequest, authRequestNas.GmmMessage.GmmHeader.GetMessageType())
	require.True(t, amfUe.State[models.AccessType__3_GPP_ACCESS].Is(amf_context.Authentication))

	authResponse := nastesting.GetAuthenticationResponse(servers.resStar, "")
	handleUplinkNASTransportMain(
		ran,
		ranUe,
		&ngapType.NASPDU{Value: authResponse},
		nil,
	)

	securityModeRaw := lastConnMessage(t, connStub)
	securityModePdu, securityModeNas := decodeNgapNas(t, securityModeRaw, amfUe, models.AccessType__3_GPP_ACCESS)
	require.Equal(t, int64(ngapType.ProcedureCodeDownlinkNASTransport), securityModePdu.InitiatingMessage.ProcedureCode.Value)
	require.Equal(t, nas.MsgTypeSecurityModeCommand, securityModeNas.GmmMessage.GmmHeader.GetMessageType())
	require.True(t, amfUe.State[models.AccessType__3_GPP_ACCESS].Is(amf_context.SecurityMode))

	securityModeComplete := encodeUplinkNas(
		t,
		amfUe,
		models.AccessType__3_GPP_ACCESS,
		nastesting.GetSecurityModeComplete(nil),
		nas.SecurityHeaderTypeIntegrityProtectedAndCipheredWithNew5gNasSecurityContext,
	)
	handleUplinkNASTransportMain(
		ran,
		ranUe,
		&ngapType.NASPDU{Value: securityModeComplete},
		nil,
	)

	registrationAcceptRaw := lastConnMessage(t, connStub)
	registrationAcceptPdu, registrationAcceptNas := decodeNgapNas(
		t,
		registrationAcceptRaw,
		amfUe,
		models.AccessType__3_GPP_ACCESS,
	)
	require.Equal(t, int64(ngapType.ProcedureCodeDownlinkNASTransport), registrationAcceptPdu.InitiatingMessage.ProcedureCode.Value)
	require.Equal(t, nas.MsgTypeRegistrationAccept, registrationAcceptNas.GmmMessage.GmmHeader.GetMessageType())
	require.True(t, amfUe.State[models.AccessType__3_GPP_ACCESS].Is(amf_context.ContextSetup))
	require.Equal(t, servers.supi, amfUe.Supi)
	require.NotEmpty(t, amfUe.Pei)
	require.True(t, amfUe.ContextValid)
	require.True(t, amfUe.UeCmRegistered[models.AccessType__3_GPP_ACCESS])
	require.NotEmpty(t, amfUe.AllowedNssai[models.AccessType__3_GPP_ACCESS])
	require.NotNil(t, amfUe.T3550)

	registrationComplete := encodeUplinkNas(
		t,
		amfUe,
		models.AccessType__3_GPP_ACCESS,
		nastesting.GetRegistrationComplete(nil),
		nas.SecurityHeaderTypeIntegrityProtectedAndCiphered,
	)
	handleUplinkNASTransportMain(
		ran,
		ranUe,
		&ngapType.NASPDU{Value: registrationComplete},
		nil,
	)

	require.True(t, amfUe.State[models.AccessType__3_GPP_ACCESS].Is(amf_context.Registered))
	require.Nil(t, amfUe.T3550)
}

type registrationMockServers struct {
	nrf     *httptest.Server
	ausf    *httptest.Server
	udm     *httptest.Server
	pcf     *httptest.Server
	supi    string
	rand    string
	resStar []byte
	hxres   string
	kseaf   string
}

type testConsumerApp struct {
	cfg *factory.Config
	ctx *amf_context.AMFContext
}

func (a *testConsumerApp) Config() *factory.Config          { return a.cfg }
func (a *testConsumerApp) Context() *amf_context.AMFContext { return a.ctx }
func (a *testConsumerApp) SetLogEnable(bool)                {}
func (a *testConsumerApp) SetLogLevel(string)               {}
func (a *testConsumerApp) SetReportCaller(bool)             {}
func (a *testConsumerApp) Start()                           {}
func (a *testConsumerApp) Terminate()                       {}

func newRegistrationMockServers(t *testing.T) *registrationMockServers {
	t.Helper()

	s := &registrationMockServers{
		supi:    "imsi-2089300007487",
		rand:    "000102030405060708090a0b0c0d0e0f",
		resStar: []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe, 0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe},
		kseaf:   "465b5ce8b199b49faa5f0a2ee238a6bc2d1fb24843a58ed0e1b90b2227a1df8d",
	}
	s.hxres = calculateHxresStar(t, s.rand, s.resStar)

	s.ausf = newH2CTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/nausf-auth/v1/ue-authentications":
			writeJSON(t, w, http.StatusCreated, models.UeAuthenticationCtx{
				AuthType: models.AusfUeAuthenticationAuthType__5_G_AKA,
				Var5gAuthData: map[string]any{
					"rand":      s.rand,
					"autn":      "11223344556677889900aabbccddeeff",
					"hxresStar": s.hxres,
				},
				Links: map[string][]models.Link{
					"5g-aka": {{
						Href: s.ausf.URL + "/nausf-auth/v1/ue-authentications/ctx-1/5g-aka-confirmation",
					}},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/nausf-auth/v1/ue-authentications/ctx-1/5g-aka-confirmation":
			writeJSON(t, w, http.StatusOK, models.ConfirmationDataResponse{
				AuthResult: models.AusfUeAuthenticationAuthResult_SUCCESS,
				Supi:       s.supi,
				Kseaf:      s.kseaf,
			})
		default:
			http.NotFound(w, r)
		}
	}))

	s.udm = newH2CTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/nudm-uecm/v1/"+s.supi+"/registrations/amf-3gpp-access":
			writeJSON(t, w, http.StatusCreated, models.Amf3GppAccessRegistration{})
		case r.Method == http.MethodGet && r.URL.Path == "/nudm-sdm/v2/"+s.supi+"/am-data":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"gpsis":     []string{"msisdn-0900000000"},
				"rfspIndex": 1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/nudm-sdm/v2/"+s.supi+"/smf-select-data":
			writeJSON(t, w, http.StatusOK, models.SmfSelectionSubscriptionData{})
		case r.Method == http.MethodGet && r.URL.Path == "/nudm-sdm/v2/"+s.supi+"/ue-context-in-smf-data":
			writeJSON(t, w, http.StatusOK, models.UeContextInSmfData{})
		case r.Method == http.MethodPost && r.URL.Path == "/nudm-sdm/v2/"+s.supi+"/sdm-subscriptions":
			w.Header().Set("Location", s.udm.URL+"/nudm-sdm/v2/"+s.supi+"/sdm-subscriptions/sub-1")
			writeJSON(t, w, http.StatusCreated, models.SdmSubscription{SubscriptionId: "sub-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/nudm-sdm/v2/"+s.supi+"/nssai":
			writeJSON(t, w, http.StatusOK, models.Nssai{
				DefaultSingleNssais: []models.Snssai{{
					Sst: 1,
					Sd:  "010203",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))

	s.pcf = newH2CTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/npcf-am-policy-control/v1/policies":
			w.Header().Set("Location", s.pcf.URL+"/npcf-am-policy-control/v1/policies/policy-1")
			writeJSON(t, w, http.StatusCreated, models.PcfAmPolicyControlPolicyAssociation{})
		default:
			http.NotFound(w, r)
		}
	}))

	s.nrf = newH2CTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nnrf-disc/v1/nf-instances" {
			http.NotFound(w, r)
			return
		}
		target := r.URL.Query().Get("target-nf-type")
		var profile models.NrfNfDiscoveryNfProfile
		switch target {
		case string(models.NrfNfManagementNfType_AUSF):
			profile = testNfProfile(uuid.NewString(), models.NrfNfManagementNfType_AUSF, models.ServiceName_NAUSF_AUTH, s.ausf.URL)
		case string(models.NrfNfManagementNfType_UDM):
			profile = testNfProfile(uuid.NewString(), models.NrfNfManagementNfType_UDM, models.ServiceName_NUDM_UECM, s.udm.URL)
			profile.NfServices = append(profile.NfServices, models.NrfNfDiscoveryNfService{
				ServiceInstanceId: uuid.NewString(),
				ServiceName:       models.ServiceName_NUDM_SDM,
				Scheme:            models.UriScheme_HTTP,
				NfServiceStatus:   models.NfServiceStatus_REGISTERED,
				ApiPrefix:         s.udm.URL,
			})
		case string(models.NrfNfManagementNfType_PCF):
			profile = testNfProfile(uuid.NewString(), models.NrfNfManagementNfType_PCF, models.ServiceName_NPCF_AM_POLICY_CONTROL, s.pcf.URL)
		default:
			t.Fatalf("unexpected target nf type %q", target)
		}
		writeJSON(t, w, http.StatusOK, models.SearchResult{NfInstances: []models.NrfNfDiscoveryNfProfile{profile}})
	}))

	return s
}

func (s *registrationMockServers) close() {
	s.nrf.Close()
	s.ausf.Close()
	s.udm.Close()
	s.pcf.Close()
}

func newH2CTestServer(handler http.Handler) *httptest.Server {
	server := httptest.NewUnstartedServer(h2c.NewHandler(handler, &http2.Server{}))
	server.EnableHTTP2 = true
	server.Start()
	return server
}

func newTestConfig() *factory.Config {
	return &factory.Config{
		Info: &factory.Info{Version: "1.0.9"},
		Configuration: &factory.Configuration{
			DefaultUECtxReq: false,
			T3550: factory.TimerValue{
				Enable:        true,
				ExpireTime:    3600000000000,
				MaxRetryTimes: 1,
			},
			T3560: factory.TimerValue{
				Enable:        false,
				ExpireTime:    0,
				MaxRetryTimes: 0,
			},
			T3570: factory.TimerValue{
				Enable:        false,
				ExpireTime:    0,
				MaxRetryTimes: 0,
			},
		},
		Logger: &factory.Logger{Enable: false, Level: "error"},
	}
}

func testSuciMobileIdentity() nasType.MobileIdentity5GS {
	return nasType.MobileIdentity5GS{
		Len:    12,
		Buffer: []uint8{0x01, 0x02, 0xf8, 0x39, 0xf0, 0xff, 0x00, 0x00, 0x00, 0x00, 0x47, 0x78},
	}
}

func testUESecurityCapability() *nasType.UESecurityCapability {
	capability := nasType.NewUESecurityCapability(nasMessage.RegistrationRequestUESecurityCapabilityType)
	capability.SetLen(2)
	capability.SetEA0_5G(1)
	capability.SetIA2_128_5G(1)
	return capability
}

func testCapability5GMM() *nasType.Capability5GMM {
	capability := nasType.NewCapability5GMM(nasMessage.RegistrationRequestCapability5GMMType)
	capability.SetLen(1)
	capability.SetS1Mode(1)
	return capability
}

func testNfProfile(id string, nfType models.NrfNfManagementNfType, serviceName models.ServiceName, apiPrefix string) models.NrfNfDiscoveryNfProfile {
	return models.NrfNfDiscoveryNfProfile{
		NfInstanceId: id,
		NfType:       nfType,
		NfStatus:     models.NrfNfManagementNfStatus_REGISTERED,
		NfServices: []models.NrfNfDiscoveryNfService{{
			ServiceInstanceId: uuid.NewString(),
			ServiceName:       serviceName,
			Scheme:            models.UriScheme_HTTP,
			NfServiceStatus:   models.NfServiceStatus_REGISTERED,
			ApiPrefix:         apiPrefix,
		}},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

func calculateHxresStar(t *testing.T, randHex string, resStar []byte) string {
	t.Helper()
	randBytes, err := hex.DecodeString(randHex)
	require.NoError(t, err)
	h := sha256.Sum256(append(randBytes, resStar...))
	return hex.EncodeToString(h[16:])
}

func lastConnMessage(t *testing.T, conn *ngaptesting.SctpConnStub) []byte {
	t.Helper()
	require.NotEmpty(t, conn.MsgList)
	return conn.MsgList[len(conn.MsgList)-1]
}

func decodeNgapNas(
	t *testing.T,
	raw []byte,
	ue *amf_context.AmfUe,
	accessType models.AccessType,
) (*ngapType.NGAPPDU, *nas.Message) {
	t.Helper()

	pdu, err := libngap.Decoder(raw)
	require.NoError(t, err)
	require.NotNil(t, pdu.InitiatingMessage)

	switch pdu.InitiatingMessage.ProcedureCode.Value {
	case ngapType.ProcedureCodeDownlinkNASTransport:
		dl := pdu.InitiatingMessage.Value.DownlinkNASTransport
		require.NotNil(t, dl)
		for _, ie := range dl.ProtocolIEs.List {
			if ie.Id.Value == ngapType.ProtocolIEIDNASPDU {
				return pdu, decodeDownlinkNas(t, ie.Value.NASPDU.Value, ue, accessType)
			}
		}
	case ngapType.ProcedureCodeInitialContextSetup:
		ics := pdu.InitiatingMessage.Value.InitialContextSetupRequest
		require.NotNil(t, ics)
		for _, ie := range ics.ProtocolIEs.List {
			if ie.Id.Value == ngapType.ProtocolIEIDNASPDU {
				return pdu, decodeDownlinkNas(t, ie.Value.NASPDU.Value, ue, accessType)
			}
		}
	}

	t.Fatalf("no NAS PDU found in NGAP message, procedure code %d", pdu.InitiatingMessage.ProcedureCode.Value)
	return nil, nil
}

func decodeDownlinkNas(
	t *testing.T,
	payload []byte,
	ue *amf_context.AmfUe,
	accessType models.AccessType,
) *nas.Message {
	t.Helper()

	msg := new(nas.Message)
	if nas.GetSecurityHeaderType(payload)&0x0f == nas.SecurityHeaderTypePlainNas {
		plain := append([]byte(nil), payload...)
		require.NoError(t, msg.PlainNasDecode(&plain))
		return msg
	}

	require.NotNil(t, ue)
	require.GreaterOrEqual(t, len(payload), 7)

	securityHeaderType := nas.GetSecurityHeaderType(payload) & 0x0f
	seqPayload := append([]byte(nil), payload[6:]...)
	sequenceNumber := seqPayload[0]
	count := ue.DLCount.Get() - 1
	if securityHeaderType == nas.SecurityHeaderTypeIntegrityProtectedAndCipheredWithNew5gNasSecurityContext {
		count = 0
	}

	if securityHeaderType == nas.SecurityHeaderTypeIntegrityProtectedAndCiphered ||
		securityHeaderType == nas.SecurityHeaderTypeIntegrityProtectedAndCipheredWithNew5gNasSecurityContext {
		require.NoError(t, nassecurity.NASEncrypt(
			ue.CipheringAlg,
			ue.KnasEnc,
			count,
			amf_nas_security.GetBearerType(accessType),
			nassecurity.DirectionDownlink,
			seqPayload[1:],
		))
	}

	require.Equal(t, uint8(count&0xff), sequenceNumber)
	plain := seqPayload[1:]
	require.NoError(t, msg.PlainNasDecode(&plain))
	return msg
}

func encodeUplinkNas(
	t *testing.T,
	ue *amf_context.AmfUe,
	accessType models.AccessType,
	plain []byte,
	securityHeaderType uint8,
) []byte {
	t.Helper()

	count := ue.ULCount.Get()
	if securityHeaderType == nas.SecurityHeaderTypeIntegrityProtectedAndCipheredWithNew5gNasSecurityContext {
		count = 0
	}

	payload := append([]byte(nil), plain...)
	if securityHeaderType == nas.SecurityHeaderTypeIntegrityProtectedAndCiphered ||
		securityHeaderType == nas.SecurityHeaderTypeIntegrityProtectedAndCipheredWithNew5gNasSecurityContext {
		require.NoError(t, nassecurity.NASEncrypt(
			ue.CipheringAlg,
			ue.KnasEnc,
			count,
			amf_nas_security.GetBearerType(accessType),
			nassecurity.DirectionUplink,
			payload,
		))
	}

	protected := append([]byte{uint8(count & 0xff)}, payload...)
	mac, err := nassecurity.NASMacCalculate(
		ue.IntegrityAlg,
		ue.KnasInt,
		count,
		amf_nas_security.GetBearerType(accessType),
		nassecurity.DirectionUplink,
		protected,
	)
	require.NoError(t, err)

	out := []byte{nasMessage.Epd5GSMobilityManagementMessage, securityHeaderType}
	out = append(out, mac...)
	out = append(out, protected...)
	return out
}
