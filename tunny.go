/*
Copyright (c) 2014 Ashley Jeffs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// Package tunny implements a simple pool for maintaining independant worker goroutines.
package tunny

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

/*
TunnyWorker - The basic interface of a tunny worker.
*/
type TunnyWorker interface {

	// Called for each job, expects the result to be returned synchronously
	TunnyJob(interface{}) interface{}

	// Called after each job, this indicates whether the worker is ready for the next job.
	// The default implementation is to return true always. If false is returned then the
	// method is called every five milliseconds until either true is returned or the pool
	// is closed.
	TunnyReady() bool
}

/*
TunnyExtendedWorker - An optional interface that can be implemented if the worker needs
more control over its state.
*/
type TunnyExtendedWorker interface {

	// Called when the pool is opened, this will be called before any jobs are sent.
	TunnyInitialize()

	// Called when the pool is closed, this will be called after all jobs are completed.
	TunnyTerminate()
}

/*
TunnyInterruptable - An optional interface that can be implemented in order to allow the
worker to drop jobs when they are abandoned.
*/
type TunnyInterruptable interface {

	// Called when the current job has been abandoned by the client.
	TunnyInterrupt()
}

/*
Default and very basic implementation of a tunny worker. This worker holds a closure which
is assigned at construction, and this closure is called on each job.
*/
type tunnyDefaultWorker struct {
	job *func(interface{}) interface{}
}

func (worker *tunnyDefaultWorker) TunnyJob(data interface{}) interface{} {
	return (*worker.job)(data)
}

func (worker *tunnyDefaultWorker) TunnyReady() bool {
	return true
}

/*
workPool contains the structures and methods required to communicate with your pool, it must
be opened before sending work and closed when all jobs are completed.

You may open and close a pool as many times as you wish, calling close is a blocking call that
guarantees all goroutines are stopped.
*/
type workPool struct {
	workers     []*workerWrapper
	selects     []reflect.SelectCase
	statusMutex sync.RWMutex
	running     uint32
}

func (pool *workPool) isRunning() bool {
	return (atomic.LoadUint32(&pool.running) == 1)
}

func (pool *workPool) setRunning(running bool) {
	if running {
		atomic.SwapUint32(&pool.running, 1)
	} else {
		atomic.SwapUint32(&pool.running, 0)
	}
}

/*
Open all channels and launch the background goroutines managed by the pool.
*/
func (pool *workPool) Open() (*workPool, error) {
	pool.statusMutex.Lock()
	defer pool.statusMutex.Unlock()

	if !pool.isRunning() {

		pool.selects = make([]reflect.SelectCase, len(pool.workers))

		for i, workerWrapper := range pool.workers {
			workerWrapper.Open()

			pool.selects[i] = reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(workerWrapper.readyChan),
			}
		}

		pool.setRunning(true)
		return pool, nil

	}
	return nil, errors.New("pool is already running")
}

/*
Close all channels and goroutines managed by the pool.
*/
func (pool *workPool) Close() error {
	pool.statusMutex.Lock()
	defer pool.statusMutex.Unlock()

	if pool.isRunning() {

		for _, workerWrapper := range pool.workers {
			workerWrapper.Close()
		}

		for _, workerWrapper := range pool.workers {
			workerWrapper.Join()
		}

		pool.setRunning(false)
		return nil

	}
	return errors.New("cannot close when the pool is not running")
}

/*
CreatePool - Creates a pool of workers, and takes a closure argument which is the action
to perform for each job.
*/
func CreatePool(numWorkers int, job func(interface{}) interface{}) *workPool {
	pool := workPool{running: 0}

	pool.workers = make([]*workerWrapper, numWorkers)
	for i := range pool.workers {
		newWorker := workerWrapper{
			worker: &(tunnyDefaultWorker{&job}),
		}
		pool.workers[i] = &newWorker
	}

	return &pool
}

/*
CreatePoolGeneric - Creates a pool of generic workers. When sending work to a pool of
generic workers you send a closure (func()) which is the job to perform.
*/
func CreatePoolGeneric(numWorkers int) *workPool {

	return CreatePool(numWorkers, func(jobCall interface{}) interface{} {
		if method, ok := jobCall.(func()); ok {
			method()
			return nil
		}
		return errors.New("generic worker not given a func()")
	})

}

/*
CreateCustomPool - Creates a pool for an array of custom workers. The custom workers
must implement TunnyWorker, and may also optionally implement TunnyExtendedWorker and
TunnyInterruptable.
*/
func CreateCustomPool(customWorkers []TunnyWorker) *workPool {
	pool := workPool{running: 0}

	pool.workers = make([]*workerWrapper, len(customWorkers))
	for i := range pool.workers {
		newWorker := workerWrapper{
			worker: customWorkers[i],
		}
		pool.workers[i] = &newWorker
	}

	return &pool
}

/*
SendWorkTimed - Send a job to a worker and return the result, this is a synchronous
call with a timeout.
*/
func (pool *workPool) SendWorkTimed(milliTimeout time.Duration, jobData interface{}) (interface{}, error) {
	pool.statusMutex.RLock()
	defer pool.statusMutex.RUnlock()

	if pool.isRunning() {
		before := time.Now()

		// Create new selectcase[] and add time out case
		selectCases := append(pool.selects[:], reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(time.After(milliTimeout * time.Millisecond)),
		})

		// Wait for workers, or time out
		if chosen, _, ok := reflect.Select(selectCases); ok {

			if chosen < (len(selectCases) - 1) {
				pool.workers[chosen].jobChan <- jobData

				// Wait for response, or time out
				select {
				case data, open := <-pool.workers[chosen].outputChan:
					if !open {
						return nil, errors.New("worker was closed before reaching a result")
					}
					return data, nil
				case <-time.After((milliTimeout * time.Millisecond) - time.Since(before)):
					/* If we time out here we also need to ensure that the output is still
					 * collected and that the worker can move on. Therefore, we fork the
					 * waiting process into a new goroutine.
					 */
					go func() {
						pool.workers[chosen].Interrupt()
						<-pool.workers[chosen].outputChan
					}()
					return nil, errors.New("request timed out whilst waiting for job to complete")
				}
			} else {
				return nil, errors.New("request timed out whilst waiting for a worker")
			}
		} else {
			return nil, errors.New("failed to find a worker")
		}
	} else {
		return nil, errors.New("pool is not running! Call Open() before sending work")
	}
}

/*
SendWorkTimedAsync - Send a timed job to a worker without blocking, and optionally
send the result to a receiving closure. You may set the closure to nil if no
further actions are required.
*/
func (pool *workPool) SendWorkTimedAsync(
	milliTimeout time.Duration,
	jobData interface{},
	after func(interface{}, error),
) {
	go func() {
		result, err := pool.SendWorkTimed(milliTimeout, jobData)
		if after != nil {
			after(result, err)
		}
	}()
}

/*
SendWork - Send a job to a worker and return the result, this is a synchronous call.
*/
func (pool *workPool) SendWork(jobData interface{}) (interface{}, error) {
	pool.statusMutex.RLock()
	defer pool.statusMutex.RUnlock()

	if pool.isRunning() {
		if chosen, _, ok := reflect.Select(pool.selects); ok && chosen >= 0 {
			pool.workers[chosen].jobChan <- jobData
			result, open := <-pool.workers[chosen].outputChan

			if !open {
				return nil, errors.New("worker was closed before reaching a result")
			}
			return result, nil
		}
		return nil, errors.New("failed to find or wait for a worker")
	}
	return nil, errors.New("pool is not running! Call Open() before sending work")
}

/*
SendWorkAsync - Send a job to a worker without blocking, and optionally send the
result to a receiving closure. You may set the closure to nil if no further actions
are required.
*/
func (pool *workPool) SendWorkAsync(jobData interface{}, after func(interface{}, error)) {
	go func() {
		result, err := pool.SendWork(jobData)
		if after != nil {
			after(result, err)
		}
	}()
}
