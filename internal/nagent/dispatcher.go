package nagent

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrQueueFull = errors.New("NAgent intent queue is full")
	ErrStopped   = errors.New("NAgent intent dispatcher is stopped")
)

type Submitter interface {
	SubmitIntent(context.Context, IntentRequest) ([]byte, error)
}

type Job struct {
	Context  context.Context
	Request  IntentRequest
	Callback func(Result)
}

type Result struct {
	Request  IntentRequest
	Response []byte
	Err      error
}

type Dispatcher struct {
	ctx      context.Context
	cancel   context.CancelFunc
	client   Submitter
	jobs     chan Job
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func NewDispatcher(parent context.Context, client Submitter, workers, queueSize int) *Dispatcher {
	if parent == nil {
		parent = context.Background()
	}
	if workers <= 0 {
		workers = 1
	}
	if queueSize < 0 {
		queueSize = 0
	}
	ctx, cancel := context.WithCancel(parent)
	dispatcher := &Dispatcher{
		ctx:    ctx,
		cancel: cancel,
		client: client,
		jobs:   make(chan Job, queueSize),
	}
	for i := 0; i < workers; i++ {
		dispatcher.wg.Add(1)
		go dispatcher.runWorker()
	}
	return dispatcher
}

func (d *Dispatcher) Submit(job Job) error {
	select {
	case <-d.ctx.Done():
		return ErrStopped
	default:
	}
	select {
	case <-d.ctx.Done():
		return ErrStopped
	case d.jobs <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

func (d *Dispatcher) Stop() {
	d.stopOnce.Do(d.cancel)
	d.wg.Wait()
}

func (d *Dispatcher) runWorker() {
	defer d.wg.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case job := <-d.jobs:
			d.execute(job)
		}
	}
}

func (d *Dispatcher) execute(job Job) {
	if d.ctx.Err() != nil {
		return
	}
	jobCtx := job.Context
	if jobCtx == nil {
		jobCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(jobCtx)
	stopParentCancel := context.AfterFunc(d.ctx, cancel)
	response, err := d.client.SubmitIntent(ctx, job.Request)
	stopParentCancel()
	cancel()
	if d.ctx.Err() != nil {
		return
	}
	if job.Callback != nil {
		job.Callback(Result{
			Request:  job.Request,
			Response: append([]byte(nil), response...),
			Err:      err,
		})
	}
}
