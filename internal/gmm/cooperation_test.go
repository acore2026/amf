package gmm

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
	"github.com/acore2026/openapi/models"
)

func TestProcessULCooperationStoresCompletedAPContainer(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 0x01
	mustAddIE(t, ul, 0x10, []byte{0x01})
	mustAddIE(t, ul, 0x18, []byte{0x01})
	apContents := mustAddAPContainerIE(t, ul, &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		Payload:            []byte{0xaa, 0xbb},
	})

	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}
	if len(dlMessages) != 1 || len(dlMessages[0]) != 2 {
		t.Fatalf("DL messages = %#v, want one message with two IEs", dlMessages)
	}
	assertDLIE(t, dlMessages[0][0], 0x10, []byte{0x01})
	assertAPContainerIE(t, dlMessages[0][1], 0, false, []byte{0xaa, 0xbb})

	ctx := ue.CooperationContext
	if ctx == nil {
		t.Fatal("CooperationContext is nil")
	}
	assertStoredIE(t, ctx.LastULIEs, 0x10, [][]byte{{0x01}})
	assertStoredIE(t, ctx.LastULIEs, 0x18, [][]byte{{0x01}})
	assertStoredIE(t, ctx.LastULIEs, 0x71, [][]byte{apContents})
	if got := ctx.NegotiatedIEs[0x10]; !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("NegotiatedIEs[0x10] = %x, want 01", got)
	}
	if got := ctx.NegotiatedIEs[0x18]; !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("NegotiatedIEs[0x18] = %x, want 01", got)
	}
	if _, ok := ctx.NegotiatedIEs[0x71]; ok {
		t.Fatal("NegotiatedIEs contains partial AP Container state")
	}
	completed := ctx.CompletedAPContainers()
	record, ok := completed[0x1234]
	if !ok || !bytes.Equal(record.Payload, []byte{0xaa, 0xbb}) {
		t.Fatalf("completed AP Container = %#v", record)
	}
}

func TestProcessULCooperationKeepsOrdinaryIEIndependentOfIncompleteAP(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	first := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	first.MessageIdentity = 0x01
	mustAddIE(t, first, 0x10, []byte{0x01})
	mustAddAPContainerIE(t, first, apFragment(1, 0, true, []byte("abc")))

	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, first)
	if err != nil {
		t.Fatalf("first process error = %v", err)
	}
	if len(dlMessages) != 1 || len(dlMessages[0]) != 1 {
		t.Fatalf("first DL messages = %#v", dlMessages)
	}
	assertDLIE(t, dlMessages[0][0], 0x10, []byte{0x01})
	if len(ue.CooperationContext.CompletedAPContainers()) != 0 {
		t.Fatal("incomplete AP Container was stored as completed")
	}

	second := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	second.MessageIdentity = 0x01
	mustAddAPContainerIE(t, second, apFragment(1, 3, false, []byte("def")))
	dlMessages, err = processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, second)
	if err != nil {
		t.Fatalf("second process error = %v", err)
	}
	if len(dlMessages) != 1 || len(dlMessages[0]) != 1 {
		t.Fatalf("second DL messages = %#v", dlMessages)
	}
	assertAPContainerIE(t, dlMessages[0][0], 0, false, []byte("abcdef"))
}

func TestProcessULCooperationGroupsOrdinaryIEOnlyWithFirstAPFragment(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 0x02
	mustAddIE(t, ul, 0x10, []byte{0x01})
	payload := bytes.Repeat([]byte{0xaa}, 1500)
	contents := mustEncodeAPContainer(t, &nasMessage.APContainer{
		ContainerType:      0x0100,
		ContainerTypePTI:   0x05,
		ContainerPayloadID: 0x1234,
		Payload:            payload,
	})
	ul.IEs = append(ul.IEs, nasMessage.NewCooperationIELegacy(0x71, contents))

	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}
	if len(dlMessages) != 2 {
		t.Fatalf("DL message count = %d, want 2", len(dlMessages))
	}
	if len(dlMessages[0]) != 2 || len(dlMessages[1]) != 1 {
		t.Fatalf("DL grouping = %#v", dlMessages)
	}
	assertDLIE(t, dlMessages[0][0], 0x10, []byte{0x01})
	assertAPContainerIE(t, dlMessages[0][1], 0, true, payload[:1400])
	assertAPContainerIE(t, dlMessages[1][0], 1400, false, payload[1400:])
}

func TestProcessULCooperationRejectsMultipleAPContainersWithoutBlockingIE10(t *testing.T) {
	ue := &context.AmfUe{}
	t.Cleanup(ue.StopAPContainerReassemblyTimers)
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 0x01
	mustAddIE(t, ul, 0x10, []byte{0x01})
	mustAddAPContainerIE(t, ul, &nasMessage.APContainer{ContainerPayloadID: 1})
	mustAddAPContainerIE(t, ul, &nasMessage.APContainer{ContainerPayloadID: 2})

	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}
	if len(dlMessages) != 1 || len(dlMessages[0]) != 1 {
		t.Fatalf("DL messages = %#v", dlMessages)
	}
	assertDLIE(t, dlMessages[0][0], 0x10, []byte{0x01})
}

func TestProcessULCooperationDoesNotEmitDL18(t *testing.T) {
	ue := &context.AmfUe{}
	ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
	ul.MessageIdentity = 0x01
	mustAddIE(t, ul, 0x18, []byte{0x01})

	dlMessages, err := processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
	if err != nil {
		t.Fatalf("processULCooperationIEs() error = %v", err)
	}
	if len(dlMessages) != 0 {
		t.Fatalf("DL message count = %d, want 0", len(dlMessages))
	}
}

func TestProcessULCooperationStateSupportsConcurrentAccesses(t *testing.T) {
	ue := &context.AmfUe{}
	const messages = 100
	errCh := make(chan error, messages)
	var wg sync.WaitGroup
	for i := 0; i < messages; i++ {
		wg.Add(1)
		go func(value byte) {
			defer wg.Done()
			ul := nasMessage.NewULCooperation(nas.MsgTypeULCooperation)
			ul.MessageIdentity = value
			ie, err := nasMessage.NewCooperationIE(0x10, []byte{value})
			if err != nil {
				errCh <- err
				return
			}
			ul.IEs = append(ul.IEs, ie)
			_, err = processULCooperationIEs(ue, models.AccessType__3_GPP_ACCESS, ul)
			errCh <- err
		}(byte(i))
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Cooperation processing failed: %v", err)
		}
	}
}

func TestLogCooperationIEBoundsAPContainerPayload(t *testing.T) {
	var output bytes.Buffer
	log := logrus.New()
	log.SetOutput(&output)
	ue := &context.AmfUe{GmmLog: logrus.NewEntry(log)}
	payload := bytes.Repeat([]byte{0xaa}, 64)
	contents := mustEncodeAPContainer(t, &nasMessage.APContainer{
		ContainerPayloadID: 1,
		Payload:            payload,
	})
	ie, err := nasMessage.NewCooperationIE(nasMessage.CooperationIEType71, contents)
	if err != nil {
		t.Fatalf("NewCooperationIE() error = %v", err)
	}

	logCooperationIE(ue, "IE[0]", ie)

	got := output.String()
	if strings.Contains(got, strings.Repeat("aa", len(payload))) {
		t.Fatal("log contains the complete AP Container payload")
	}
	if !strings.Contains(got, "payloadLength=64") {
		t.Fatalf("log does not contain AP metadata: %s", got)
	}
}

func mustAddIE(t *testing.T, ul *nasMessage.ULCooperation, iei uint8, contents []byte) {
	t.Helper()
	if err := ul.AddIE(iei, contents); err != nil {
		t.Fatalf("AddIE(0x%02x) error = %v", iei, err)
	}
}

func mustAddAPContainerIE(
	t *testing.T,
	ul *nasMessage.ULCooperation,
	container *nasMessage.APContainer,
) []byte {
	t.Helper()
	contents := mustEncodeAPContainer(t, container)
	if err := ul.AddIE(nasMessage.CooperationIEType71, contents); err != nil {
		t.Fatalf("AddIE(0x71) error = %v", err)
	}
	return contents
}

func mustEncodeAPContainer(t *testing.T, container *nasMessage.APContainer) []byte {
	t.Helper()
	contents, err := container.Encode()
	if err != nil {
		t.Fatalf("APContainer.Encode() error = %v", err)
	}
	return contents
}

func assertStoredIE(t *testing.T, got map[uint8][][]byte, iei uint8, want [][]byte) {
	t.Helper()
	values := got[iei]
	if len(values) != len(want) {
		t.Fatalf("LastULIEs[0x%02x] count = %d, want %d", iei, len(values), len(want))
	}
	for i := range want {
		if !bytes.Equal(values[i], want[i]) {
			t.Fatalf("LastULIEs[0x%02x][%d] = %x, want %x", iei, i, values[i], want[i])
		}
	}
}

func assertDLIE(t *testing.T, ie *nasMessage.CooperationIE, wantIEI uint8, wantContents []byte) {
	t.Helper()
	if ie == nil {
		t.Fatalf("DL IE 0x%02x is nil", wantIEI)
	}
	if ie.GetIei() != wantIEI {
		t.Fatalf("DL IEI = 0x%02x, want 0x%02x", ie.GetIei(), wantIEI)
	}
	if got := ie.GetContents(); !bytes.Equal(got, wantContents) {
		t.Fatalf("DL IE 0x%02x contents = %x, want %x", wantIEI, got, wantContents)
	}
}

func assertAPContainerIE(
	t *testing.T,
	ie *nasMessage.CooperationIE,
	wantOffset uint16,
	wantMF bool,
	wantPayload []byte,
) {
	t.Helper()
	if ie == nil || ie.GetIei() != nasMessage.CooperationIEType71 {
		t.Fatalf("AP Container IE = %#v", ie)
	}
	container, err := nasMessage.DecodeAPContainer(ie.GetContents())
	if err != nil {
		t.Fatalf("DecodeAPContainer() error = %v", err)
	}
	if container.FragmentOffset != wantOffset || container.MoreFragments() != wantMF ||
		!bytes.Equal(container.Payload, wantPayload) {
		t.Fatalf("AP Container = %#v, want offset=%d MF=%v payload=%x",
			container, wantOffset, wantMF, wantPayload)
	}
}
