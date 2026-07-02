package nas_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	acoreNas "github.com/acore2026/nas"
	"github.com/acore2026/nas/nasMessage"
)

func TestParseRealUEMessageV2(t *testing.T) {
	// 完整的UE数据
	rawData := []byte{
		0x00, 0x02, 0x00, 0x84, 0x00, 0x26, 0x00, 0x80, 0x84, 0x80, 0x82,
		0x7e, 0x02, 0x94, 0x42, 0x86, 0x51, 0x0a, 0x7e, 0x00, 0xe1, 0x02,
		0x71, 0x00, 0x74, 0x7f, 0x00, 0xe1, 0x02, 0x71, 0x00, 0x6d, 0x01,
		0x00, 0x00, 0x69, 0x03, 0x7b, 0x22, 0x69, 0x6e, 0x74, 0x65, 0x6e,
		0x74, 0x49, 0x64, 0x22, 0x3a, 0x22, 0x65, 0x64, 0x66, 0x63, 0x34,
		0x31, 0x32, 0x39, 0x2d, 0x38, 0x36, 0x32, 0x33, 0x2d, 0x34, 0x36,
		0x63, 0x31, 0x2d, 0x39, 0x31, 0x63, 0x31, 0x2d, 0x33, 0x31, 0x64,
		0x63, 0x38, 0x35, 0x66, 0x62, 0x62, 0x38, 0x37, 0x66, 0x22, 0x2c,
		0x22, 0x69, 0x6e, 0x74, 0x65, 0x6e, 0x74, 0x44, 0x65, 0x73, 0x63,
		0x72, 0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x22, 0x3a, 0x22, 0x48,
		0x65, 0x6c, 0x6c, 0x6f, 0x22, 0x2c, 0x22, 0x6f, 0x62, 0x6a, 0x65,
		0x63, 0x74, 0x22, 0x3a, 0x22, 0x4e, 0x65, 0x77, 0x4e, 0x41, 0x53,
		0x41, 0x64, 0x61, 0x70, 0x74, 0x65, 0x72, 0x22, 0x7d, 0x00, 0x79,
		0x40, 0x0f, 0x40, 0x00, 0xf1, 0x10, 0x00, 0x00, 0x01, 0x14, 0xd0,
		0x00, 0xf1, 0x10, 0x00, 0x00, 0x01,
	}

	t.Logf("=== 原始UE数据 (%d 字节) ===", len(rawData))
	t.Logf("%s", hex.Dump(rawData[:60]))

	// 构造标准NAS格式（添加SecurityHeader字节）
	// 从offset 18开始: 00 e1 02 ...
	// 构造: 00 00 e1 02 ... (添加0x00作为SecurityHeader)
	t.Logf("\n=== 构造标准NAS消息并使用V2解析（1字节Length） ===")
	nasData := make([]byte, 0, len(rawData)-18+1)
	nasData = append(nasData, 0x00)  // EPD (5GS MM)
	nasData = append(nasData, 0x00)  // Security Header Type = 0 (no security)
	nasData = append(nasData, rawData[19:]...)  // 从offset 19开始：e1 02 71 ...
	
	t.Logf("构造后的NAS消息 (%d 字节):", len(nasData))
	t.Logf("%s", hex.Dump(nasData[:40]))

	ul := nasMessage.NewULCooperation(acoreNas.MsgTypeULCooperation)
	err := ul.DecodeULCooperationV2(&nasData)
	if err != nil {
		t.Fatalf("DecodeULCooperationV2失败: %v", err)
	}

	t.Logf("\n✓ 解析成功!")
	t.Logf("  MessageType: 0x%02x", ul.MessageType)
	t.Logf("  ULApContainer: %v", ul.ULApContainer != nil)
	if ul.ULApContainer != nil {
		contents := ul.ULApContainer.GetContents()
		if len(contents) > 0 && contents[0] == 0x7b {
			t.Logf("    ULApContainer JSON内容: %s", string(contents))
		} else {
			t.Logf("    ULApContainer 内容: %s", hex.EncodeToString(contents))
		}
	}
	t.Logf("  UnknownIEs数量: %d", len(ul.UnknownIEs))
	
	for i, ie := range ul.UnknownIEs {
		t.Logf("  UnknownIE[%d]: IEI=0x%02x Len=%d", i, ie.GetIei(), ie.GetLen())
		contents := ie.GetContents()
		if len(contents) > 0 && contents[0] == 0x7b {
			t.Logf("    JSON内容: %s", string(contents))
		} else if len(contents) > 20 {
			t.Logf("    Hex内容(前20字节): %s...", hex.EncodeToString(contents[:20]))
		} else {
			t.Logf("    Hex内容: %s", hex.EncodeToString(contents))
		}
	}
}

func TestPlainNasDecodeULCooperation(t *testing.T) {
	pdu := []byte{
		nasMessage.Epd5GSMobilityManagementMessage, 0x00, acoreNas.MsgTypeULCooperation,
		nasMessage.ULCooperationULApContainerType, 0x00, 0x03, 0x10, 0x20, 0x30,
	}

	msg := acoreNas.NewMessage()
	if err := msg.PlainNasDecode(&pdu); err != nil {
		t.Fatalf("PlainNasDecode() error = %v", err)
	}
	if msg.GmmMessage == nil || msg.GmmMessage.ULCooperation == nil {
		t.Fatalf("ULCooperation was not decoded: %#v", msg.GmmMessage)
	}
	ul := msg.GmmMessage.ULCooperation
	if ul.MessageType != acoreNas.MsgTypeULCooperation {
		t.Fatalf("message type = 0x%02x, want 0x%02x", ul.MessageType, acoreNas.MsgTypeULCooperation)
	}
	if ul.ULApContainer == nil {
		t.Fatalf("ULApContainer is nil")
	}
	if got := ul.ULApContainer.GetContents(); !bytes.Equal(got, []byte{0x10, 0x20, 0x30}) {
		t.Fatalf("ULApContainer = %x, want 10 20 30", got)
	}
}