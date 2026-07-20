package context

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/acore2026/amf/internal/logger"
)

type panicWriteConn struct {
	net.Conn
}

func (panicWriteConn) Write([]byte) (int, error) {
	panic("write failed")
}

func (panicWriteConn) SetWriteDeadline(time.Time) error {
	return nil
}

func TestWritePacketAppliesWriteDeadline(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	ran := &AmfRan{Conn: client, Log: logger.NgapLog}

	started := time.Now()
	n, err := ran.WritePacket([]byte{1}, 20*time.Millisecond)
	require.Error(t, err)
	require.Zero(t, n)
	require.Less(t, time.Since(started), time.Second)
}

func TestWritePacketConvertsConnectionPanicToError(t *testing.T) {
	ran := &AmfRan{Conn: panicWriteConn{}, Log: logger.NgapLog}
	n, err := ran.WritePacket([]byte{1}, time.Second)
	require.ErrorContains(t, err, "write panic")
	require.Zero(t, n)
}

func TestRemoveAndRemoveAllRanUeRaceCondition(t *testing.T) {
	ran := &AmfRan{
		Log: logger.NgapLog.WithField("", ""),
	}

	// create ranUe & store in RanUeList
	for i := 1; i <= 10000; i++ {
		ranUe, err := ran.NewRanUe(int64(i))
		require.NoError(t, err)
		ran.RanUeList.Store(i, ranUe)
	}

	require.NotPanics(t, func() { runRanUeRemove(ran) })
}

func runRanUeRemove(ran *AmfRan) {
	for i := 1; i <= 10000; i++ {
		go ran.RanUeList.Delete(i)
	}
	ran.RemoveAllRanUe(true)
}
