// Copyright (c) 2014 Ashley Jeffs
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package tunny

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

//------------------------------------------------------------------------------

// Errors that are used throughout the Tunny API.
var (
	ErrPoolNotRunning = errors.New("the pool is not running")
	ErrJobNotFunc     = errors.New("generic worker not given a func()")
	ErrWorkerClosed   = errors.New("worker was closed")
	ErrJobTimedOut    = errors.New("job request timed out")
)

// Worker is an interface representing a Tunny working agent. It will be used to
// block a calling goroutine until ready to process a job, process that job
// synchronously, interrupt its own process call when jobs are abandoned, and
// clean up its resources when being removed from the pool.
//
// Each of these duties are implemented as a single method and can be averted
// when not needed by simply implementing an empty func.
type Worker[T, U any] interface {
	// Process will synchronously perform a job and return the result.
	Process(T) (U, error)

	// BlockUntilReady is called before each job is processed and must block the
	// calling goroutine until the Worker is ready to process the next job.
	BlockUntilReady()

	// Interrupt is called when a job is cancelled. The worker is responsible
	// for unblocking the Process implementation.
	Interrupt()

	// Terminate is called when a Worker is removed from the processing pool
	// and is responsible for cleaning up any held resources.
	Terminate()
}

//------------------------------------------------------------------------------

// closureWorker is a minimal Worker implementation that simply wraps a
// func(interface{}) interface{}
type closureWorker[T, U any] struct {
	processor func(T) (U, error)
}

func (w *closureWorker[T, U]) Process(payload T) (U, error) {
	return w.processor(payload)
}

func (w *closureWorker[T, U]) BlockUntilReady() {}
func (w *closureWorker[T, U]) Interrupt()       {}
func (w *closureWorker[T, U]) Terminate()       {}

//------------------------------------------------------------------------------

// callbackWorker is a minimal Worker implementation that attempts to cast
// each job into func() and either calls it if successful or returns
// ErrJobNotFunc.
type callbackWorker[T, U any] struct{}

func (w *callbackWorker[T, U]) Process(payload T) (ret U, err error) {
	f, ok := (interface{})(payload).(func())
	if !ok {
		err = ErrJobNotFunc
		return
	}
	f()
	return
}

func (w *callbackWorker[T, U]) BlockUntilReady() {}
func (w *callbackWorker[T, U]) Interrupt()       {}
func (w *callbackWorker[T, U]) Terminate()       {}

//------------------------------------------------------------------------------

// Pool is a struct that manages a collection of workers, each with their own
// goroutine. The Pool can initialize, expand, compress and close the workers,
// as well as processing jobs with the workers synchronously.
type Pool[T, U any] struct {
	queuedJobs int64

	ctor    func() Worker[T, U]
	workers []*workerWrapper[T, U]
	reqChan chan workRequest[T, U]

	workerMut sync.Mutex
}

// New creates a new Pool of workers that starts with n workers. You must
// provide a constructor function that creates new Worker types and when you
// change the size of the pool the constructor will be called to create each new
// Worker.
func New[T, U any](n int, ctor func() Worker[T, U]) *Pool[T, U] {
	p := &Pool[T, U]{
		ctor:    ctor,
		reqChan: make(chan workRequest[T, U]),
	}
	p.SetSize(n)

	return p
}

// NewFunc creates a new Pool of workers where each worker will process using
// the provided func.
func NewFunc[T, U any](n int, f func(T) (U, error)) *Pool[T, U] {
	return New(n, func() Worker[T, U] {
		return &closureWorker[T, U]{
			processor: f,
		}
	})
}

// NewCallback creates a new Pool of workers where workers cast the job payload
// into a func() and runs it, or returns ErrNotFunc if the cast failed.
func NewCallback[T, U any](n int) *Pool[T, U] {
	return New(n, func() Worker[T, U] {
		return &callbackWorker[T, U]{}
	})
}

//------------------------------------------------------------------------------

// Process will use the Pool to process a payload and synchronously return the
// result. Process can be called safely by any goroutines, but will panic if the
// Pool has been stopped.
func (p *Pool[T, U]) Process(payload T) (U, error) {
	atomic.AddInt64(&p.queuedJobs, 1)

	request, open := <-p.reqChan
	if !open {
		panic(ErrPoolNotRunning)
	}

	request.jobChan <- struct {
		data T
		err  error
	}{payload, nil}

	var payload2 struct {
		data U
		err  error
	}
	payload2, open = <-request.retChan
	if !open {
		panic(ErrWorkerClosed)
	}

	atomic.AddInt64(&p.queuedJobs, -1)
	return payload2.data, payload2.err
}

// ProcessTimed will use the Pool to process a payload and synchronously return
// the result. If the timeout occurs before the job has finished the worker will
// be interrupted and ErrJobTimedOut will be returned. ProcessTimed can be
// called safely by any goroutines.
func (p *Pool[T, U]) ProcessTimed(
	payload T,
	timeout time.Duration,
) (ret U, err error) {
	atomic.AddInt64(&p.queuedJobs, 1)
	defer atomic.AddInt64(&p.queuedJobs, -1)

	tout := time.NewTimer(timeout)

	var request workRequest[T, U]
	var open bool

	select {
	case request, open = <-p.reqChan:
		if !open {
			err = ErrPoolNotRunning
			return
		}
	case <-tout.C:
		err = ErrJobTimedOut
		return
	}

	select {
	case request.jobChan <- struct {
		data T
		err  error
	}{payload, nil}:
	case <-tout.C:
		request.interruptFunc()
		err = ErrJobTimedOut
		return
	}

	var payload2 struct {
		data U
		err  error
	}
	select {
	case payload2, open = <-request.retChan:
		if !open {
			err = ErrWorkerClosed
			return
		}
	case <-tout.C:
		request.interruptFunc()
		err = ErrJobTimedOut
		return
	}

	tout.Stop()
	return payload2.data, payload2.err
}

// ProcessCtx will use the Pool to process a payload and synchronously return
// the result. If the context cancels before the job has finished the worker will
// be interrupted and ErrJobTimedOut will be returned. ProcessCtx can be
// called safely by any goroutines.
func (p *Pool[T, U]) ProcessCtx(ctx context.Context, payload T) (ret U, err error) {
	atomic.AddInt64(&p.queuedJobs, 1)
	defer atomic.AddInt64(&p.queuedJobs, -1)

	var request workRequest[T, U]
	var open bool

	select {
	case request, open = <-p.reqChan:
		if !open {
			err = ErrPoolNotRunning
			return
		}
	case <-ctx.Done():
		err = ctx.Err()
		return
	}

	select {
	case request.jobChan <- struct {
		data T
		err  error
	}{payload, nil}:
	case <-ctx.Done():
		request.interruptFunc()
		err = ctx.Err()
		return
	}

	var payload2 struct {
		data U
		err  error
	}
	select {
	case payload2, open = <-request.retChan:
		if !open {
			err = ErrWorkerClosed
			return
		}
	case <-ctx.Done():
		request.interruptFunc()
		err = ctx.Err()
		return
	}
	return payload2.data, payload2.err
}

// QueueLength returns the current count of pending queued jobs.
func (p *Pool[T, U]) QueueLength() int64 {
	return atomic.LoadInt64(&p.queuedJobs)
}

// SetSize changes the total number of workers in the Pool. This can be called
// by any goroutine at any time unless the Pool has been stopped, in which case
// a panic will occur.
func (p *Pool[T, U]) SetSize(n int) {
	p.workerMut.Lock()
	defer p.workerMut.Unlock()

	lWorkers := len(p.workers)
	if lWorkers == n {
		return
	}

	// Add extra workers if N > len(workers)
	for i := lWorkers; i < n; i++ {
		p.workers = append(p.workers, newWorkerWrapper(p.reqChan, p.ctor()))
	}

	// Asynchronously stop all workers > N
	for i := n; i < lWorkers; i++ {
		p.workers[i].stop()
	}

	// Synchronously wait for all workers > N to stop
	for i := n; i < lWorkers; i++ {
		p.workers[i].join()
		p.workers[i] = nil
	}

	// Remove stopped workers from slice
	p.workers = p.workers[:n]
}

// GetSize returns the current size of the pool.
func (p *Pool[T, U]) GetSize() int {
	p.workerMut.Lock()
	defer p.workerMut.Unlock()

	return len(p.workers)
}

// Close will terminate all workers and close the job channel of this Pool.
func (p *Pool[T, U]) Close() {
	p.SetSize(0)
	close(p.reqChan)
}

//------------------------------------------------------------------------------
