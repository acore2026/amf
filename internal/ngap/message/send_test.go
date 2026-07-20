package message

import (
	"net"
	"testing"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/logger"
)

type shortWriteConn struct {
	net.Conn
}

func (shortWriteConn) Write(packet []byte) (int, error) {
	return len(packet) - 1, nil
}

func TestSendToRanRejectsShortWrite(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	ran := &context.AmfRan{
		Conn: shortWriteConn{Conn: client},
		Log:  logger.NgapLog,
	}
	if sent, cause := SendToRan(ran, []byte{1, 2}); sent || cause == "" {
		t.Fatalf("short write result sent=%v cause=%q", sent, cause)
	}
}

func TestSendDownlinkNasTransportWithResultReportsPreconditionFailure(t *testing.T) {
	if sent, cause := SendDownlinkNasTransportWithResult(nil, []byte{1}, nil); sent || cause == "" {
		t.Fatalf("nil RanUe result sent=%v cause=%q", sent, cause)
	}
	ranUe := &context.RanUe{Log: logger.NgapLog}
	if sent, cause := SendDownlinkNasTransportWithResult(ranUe, nil, nil); sent || cause == "" {
		t.Fatalf("empty NAS result sent=%v cause=%q", sent, cause)
	}
}
