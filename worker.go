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

package tunny

import (
    "sync/atomic"
    "time"
)

type workerWrapper struct {
	readyChan  chan int
	jobChan    chan interface{}
	outputChan chan interface{}
	poolOpen   uint32
	worker     TunnyWorker
}

/* TODO: As long as Ready is able to lock this loop entirely we cannot
 * guarantee that all go routines stop at pool.Close(), which totally
 * stinks.
 */
func (wrapper *workerWrapper) Loop() {

	for !wrapper.worker.Ready() {
		// It's sad that we can't simply check if jobChan is closed here.
		if atomic.LoadUint32(&(*wrapper).poolOpen) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	wrapper.readyChan <- 1

	for data := range wrapper.jobChan {
		wrapper.outputChan <- wrapper.worker.Job( data )
		for !wrapper.worker.Ready() {
			if atomic.LoadUint32(&(*wrapper).poolOpen) == 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		wrapper.readyChan <- 1
	}

	close(wrapper.readyChan)
	close(wrapper.outputChan)
}

func (wrapper *workerWrapper) Open() {
	if extWorker, ok := (*wrapper).worker.(TunnyExtendedWorker); ok {
		extWorker.Initialize()
	}

	(*wrapper).readyChan  = make (chan int)
	(*wrapper).jobChan    = make (chan interface{})
	(*wrapper).outputChan = make (chan interface{})

	atomic.SwapUint32(&(*wrapper).poolOpen, uint32(1))

	go (*wrapper).Loop()
}

func (wrapper *workerWrapper) Close() {
	close(wrapper.jobChan)

	atomic.SwapUint32(&(*wrapper).poolOpen, uint32(0))

	if extWorker, ok := (*wrapper).worker.(TunnyExtendedWorker); ok {
		extWorker.Terminate()
	}
}
