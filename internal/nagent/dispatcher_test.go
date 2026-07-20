package nagent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type blockingSubmitter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSubmitter) SubmitIntent(ctx context.Context, request IntentRequest) ([]byte, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return request.Payload, nil
	}
}

func TestDispatcherBoundsQueueAndCallsBack(t *testing.T) {
	submitter := &blockingSubmitter{started: make(chan struct{}), release: make(chan struct{})}
	dispatcher := NewDispatcher(context.Background(), submitter, 1, 1)
	defer dispatcher.Stop()

	result := make(chan Result, 2)
	if err := dispatcher.Submit(Job{Request: validIntentRequest(), Callback: func(r Result) { result <- r }}); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	<-submitter.started
	second := validIntentRequest()
	second.PayloadID = 2
	if err := dispatcher.Submit(Job{Request: second, Callback: func(r Result) { result <- r }}); err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}
	third := validIntentRequest()
	third.PayloadID = 3
	if err := dispatcher.Submit(Job{Request: third}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third Submit() error = %v, want ErrQueueFull", err)
	}

	close(submitter.release)
	for i := 0; i < 2; i++ {
		select {
		case got := <-result:
			if got.Err != nil {
				t.Fatalf("callback error = %v", got.Err)
			}
		case <-time.After(time.Second):
			t.Fatal("callback not invoked")
		}
	}
}

func TestDispatcherJobContextCancelsRequest(t *testing.T) {
	submitter := &blockingSubmitter{started: make(chan struct{}), release: make(chan struct{})}
	dispatcher := NewDispatcher(context.Background(), submitter, 1, 1)
	defer dispatcher.Stop()

	jobCtx, cancel := context.WithCancel(context.Background())
	result := make(chan Result, 1)
	if err := dispatcher.Submit(Job{
		Context:  jobCtx,
		Request:  validIntentRequest(),
		Callback: func(r Result) { result <- r },
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-submitter.started
	cancel()
	select {
	case got := <-result:
		if !errors.Is(got.Err, context.Canceled) {
			t.Fatalf("callback error = %v, want canceled", got.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback not invoked")
	}
}

func TestDispatcherStopDoesNotInvokeBusinessCallback(t *testing.T) {
	submitter := &blockingSubmitter{started: make(chan struct{}), release: make(chan struct{})}
	dispatcher := NewDispatcher(context.Background(), submitter, 1, 1)
	callback := make(chan Result, 1)
	if err := dispatcher.Submit(Job{
		Request:  validIntentRequest(),
		Callback: func(result Result) { callback <- result },
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-submitter.started
	dispatcher.Stop()

	select {
	case result := <-callback:
		t.Fatalf("callback invoked during shutdown: %#v", result)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDispatcherStopDropsQueuedJobsWithoutCallbacks(t *testing.T) {
	submitter := &blockingSubmitter{started: make(chan struct{}), release: make(chan struct{})}
	dispatcher := NewDispatcher(context.Background(), submitter, 1, 1)
	callback := make(chan Result, 2)
	if err := dispatcher.Submit(Job{
		Request:  validIntentRequest(),
		Callback: func(result Result) { callback <- result },
	}); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	<-submitter.started
	second := validIntentRequest()
	second.PayloadID++
	if err := dispatcher.Submit(Job{
		Request:  second,
		Callback: func(result Result) { callback <- result },
	}); err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}

	dispatcher.Stop()
	select {
	case result := <-callback:
		t.Fatalf("callback invoked during shutdown: %#v", result)
	case <-time.After(50 * time.Millisecond):
	}
}
