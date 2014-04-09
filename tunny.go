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
	"reflect"
	"errors"
	"time"
	"sync"
)

type TunnyWorker interface {
	Job(interface{}) (interface{})
	Ready() bool
}

type TunnyExtendedWorker interface {
	Job(interface{}) (interface{})
	Ready() bool
	Initialize()
	Terminate()
}

// Default implementation of worker

type tunnyDefaultWorker struct {
	job *func(interface{}) (interface{})
}

func (worker *tunnyDefaultWorker) Job(data interface{}) interface{} {
	return (*worker.job)(data)
}

func (worker *tunnyDefaultWorker) Ready() bool {
	return true
}

/*
WorkPool allows you to contain and send work to your worker pool.
You must first indicate that the pool should run by calling Open(), then send work to the workers
through SendWork.
*/
type WorkPool struct {
	workers []*workerWrapper
	selects []reflect.SelectCase
	mutex   sync.RWMutex
	running bool
}

/*
SendWorkTimed - Send a job to a worker and return the result, this is a blocking call with a timeout.
SendWorkTimed - Args:    milliTimeout time.Duration, jobData interface{}
SendWorkTimed - Summary: the timeout period in milliseconds, the input data for the worker to process
*/
func (pool *WorkPool) SendWorkTimed(milliTimeout time.Duration, jobData interface{}) (interface{}, error) {
	pool.mutex.RLock()
	defer pool.mutex.RUnlock()

	if pool.running {
		before := time.Now()

		// Create new selectcase[] and add time out case
		selectCases := append(pool.selects[:], reflect.SelectCase {
			Dir: reflect.SelectRecv,
			Chan: reflect.ValueOf(time.After(milliTimeout * time.Millisecond)),
		})

		// Wait for workers, or time out
		if chosen, _, ok := reflect.Select(selectCases); ok {
			if chosen < ( len(selectCases) - 1 ) {
				(*pool.workers[chosen]).jobChan <- jobData

				// Wait for response, or time out
				select {
				case data := <-(*pool.workers[chosen]).outputChan:
					return data, nil
				case <- time.After((milliTimeout * time.Millisecond) - time.Since(before)):
					/* If we time out here we also need to ensure that the output is still
					 * collected and that the worker can move on. Therefore, we fork the
					 * waiting process into a new goroutine.
					 */
					go func() {
						<-(*pool.workers[chosen]).outputChan
					}()
					return nil, errors.New("Request timed out whilst waiting for job to complete")
				}
			} else {
				return nil, errors.New("Request timed out whilst waiting for a worker")
			}
		} else {
			return nil, errors.New("Failed to find a worker")
		}
	} else {
		return nil, errors.New("Pool is not running! Call Open() before sending work")
	}
}

/*
SendWorkTimedAsync - Send a timed job to a worker without blocking, and send the result to a receiving closure.
SendWorkTimedAsync - Args:    milliTimeout time.Duration, jobData interface{}, after func(interface{}, error)
SendWorkTimedAsync - Summary: the timeout period in milliseconds
SendWorkTimedAsync - Summary: the input data for the worker to process
SendWorkTimedAsync - Summary: the closure to hand the result to
*/
func (pool *WorkPool) SendWorkTimedAsync(
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
SendWork - Send a job to a worker and return the result, this is a blocking call.
SendWork - Args:    jobData interface{}
SendWork - Summary: the input data for the worker to process
*/
func (pool *WorkPool) SendWork(jobData interface{}) (interface{}, error) {
	pool.mutex.RLock()
	defer pool.mutex.RUnlock()

	if pool.running {

		if chosen, _, ok := reflect.Select(pool.selects); ok && chosen >= 0 {
			(*pool.workers[chosen]).jobChan <- jobData
			return <- (*pool.workers[chosen]).outputChan, nil
		}

		return nil, errors.New("Failed to find or wait for a worker")

	} else {
		return nil, errors.New("Pool is not running! Call Open() before sending work")
	}
}

/*
SendWorkAsync - Send a job to a worker without blocking, and send the result to a receiving closure.
SendWorkAsync - Args:    jobData interface{}, after func(interface{}, error)
SendWorkAsync - Summary: the input data for the worker to process, the closure to hand the result to
*/
func (pool *WorkPool) SendWorkAsync(jobData interface{}, after func(interface{}, error)) {
	go func() {
		result, err := pool.SendWork(jobData)
		if after != nil {
			after(result, err)
		}
	}()
}

/*
Open - Open all channels and launch the background goroutines managed by the pool.
*/
func (pool *WorkPool) Open() (*WorkPool, error) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	if !pool.running {

		pool.selects = make( []reflect.SelectCase, len(pool.workers) )

		for i, workerWrapper := range pool.workers {
			(*workerWrapper).Open()

			pool.selects[i] = reflect.SelectCase {
				Dir: reflect.SelectRecv,
				Chan: reflect.ValueOf((*workerWrapper).readyChan),
			}
		}

		pool.running = true
		return pool, nil

	} else {
		return nil, errors.New("Pool is already running!")
	}
}

/*
Close - Close all channels and goroutines managed by the pool.
*/
func (pool *WorkPool) Close() error {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	if pool.running {

		for _, workerWrapper := range pool.workers {
			workerWrapper.Close()
		}

		for _, workerWrapper := range pool.workers {
			workerWrapper.Join()
		}

		pool.running = false
		return nil

	} else {
		return errors.New("Cannot close when the pool is not running!")
	}
}

/*
CreatePool - Creates a pool of workers.
CreatePool - Args:    numWorkers int,    job func(interface{}) (interface{})
CreatePool - Summary: number of workers, the closure to run for each job
*/
func CreatePool(numWorkers int, job func(interface{}) interface{}) *WorkPool {
	pool := WorkPool { running: false }

	pool.workers = make ([]*workerWrapper, numWorkers)
	for i, _ := range pool.workers {
		newWorker := workerWrapper {
			worker: &(tunnyDefaultWorker { &job }),
		}
		pool.workers[i] = &newWorker
	}

	return &pool
}

/*
CreatePoolGeneric - Creates a pool of generic workers, they take a func as their only argument and execute it.
CreatePoolGeneric - Args:    numWorkers int
CreatePoolGeneric - Summary: number of workers
*/
func CreatePoolGeneric(numWorkers int) *WorkPool {

	return CreatePool(numWorkers, func (jobCall interface{}) interface{} {
		if method, ok := jobCall.(func()); ok {
			method()
			return nil
		}
		return errors.New("Generic worker not given a func()")
	})

}

/*
CreateCustomPool - Creates a pool for an array of custom workers.
CreateCustomPool - Args:    customWorkers []TunnyWorker
CreateCustomPool - Summary: An array of workers to use in the pool, each worker gets its own goroutine
*/
func CreateCustomPool(customWorkers []TunnyWorker) *WorkPool {
	pool := WorkPool { running: false }

	pool.workers = make ([]*workerWrapper, len(customWorkers))
	for i, _ := range pool.workers {
		newWorker := workerWrapper {
			worker: customWorkers[i],
		}
		pool.workers[i] = &newWorker
	}

	return &pool
}
