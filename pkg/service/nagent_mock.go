package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

type embeddedNAgentMock struct {
	server   *http.Server
	listener net.Listener
}

func newEmbeddedNAgentMock(address string, handler http.Handler) (*embeddedNAgentMock, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &embeddedNAgentMock{
		listener: listener,
		server: &http.Server{
			Addr:              address,
			Handler:           handler,
			ReadHeaderTimeout: 2 * time.Second,
			ReadTimeout:       3 * time.Second,
			WriteTimeout:      3 * time.Second,
			IdleTimeout:       30 * time.Second,
		},
	}, nil
}

func (m *embeddedNAgentMock) Serve() error {
	if m == nil || m.server == nil || m.listener == nil {
		return nil
	}
	err := m.server.Serve(m.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (m *embeddedNAgentMock) Shutdown(ctx context.Context) error {
	if m == nil || m.server == nil {
		return nil
	}
	err := m.server.Shutdown(ctx)
	if err == nil {
		return nil
	}
	if closeErr := m.server.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func (m *embeddedNAgentMock) Address() string {
	if m == nil || m.listener == nil {
		return ""
	}
	return m.listener.Addr().String()
}
