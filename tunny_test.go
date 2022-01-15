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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

//------------------------------------------------------------------------------

func TestPoolSizeAdjustment(t *testing.T) {
	pool := NewFunc(10, func(interface{}) (interface{}, error) { return "foo", nil })
	if exp, act := 10, len(pool.workers); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}

	pool.SetSize(10)
	if exp, act := 10, pool.GetSize(); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}

	pool.SetSize(9)
	if exp, act := 9, pool.GetSize(); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}

	pool.SetSize(10)
	if exp, act := 10, pool.GetSize(); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}

	pool.SetSize(0)
	if exp, act := 0, pool.GetSize(); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}

	pool.SetSize(10)
	if exp, act := 10, pool.GetSize(); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}

	// Finally, make sure we still have actual active workers.
	v, err := pool.Process(0)
	if err != nil {
		t.Errorf("Should not got error: %v", err)
	}
	if exp, act := "foo", v.(string); exp != act {
		t.Errorf("Wrong result: %v != %v", act, exp)
	}

	pool.Close()
	if exp, act := 0, pool.GetSize(); exp != act {
		t.Errorf("Wrong size of pool: %v != %v", act, exp)
	}
}

//------------------------------------------------------------------------------

func TestFuncJob(t *testing.T) {
	pool := NewFunc(10, func(in interface{}) (interface{}, error) {
		intVal := in.(int)
		return intVal * 2, nil
	})
	defer pool.Close()

	for i := 0; i < 10; i++ {
		ret, err := pool.Process(10)
		if err != nil {
			t.Errorf("Should not got error: %v", err)
		}
		if exp, act := 20, ret.(int); exp != act {
			t.Errorf("Wrong result: %v != %v", act, exp)
		}
	}
}

func TestFuncJobTimed(t *testing.T) {
	pool := NewFunc(10, func(in interface{}) (interface{}, error) {
		intVal := in.(int)
		return intVal * 2, nil
	})
	defer pool.Close()

	for i := 0; i < 10; i++ {
		ret, err := pool.ProcessTimed(10, time.Millisecond)
		if err != nil {
			t.Fatalf("Failed to process: %v", err)
		}
		if exp, act := 20, ret.(int); exp != act {
			t.Errorf("Wrong result: %v != %v", act, exp)
		}
	}
}

func TestFuncJobCtx(t *testing.T) {
	t.Run("Completes when ctx not canceled", func(t *testing.T) {
		pool := NewFunc(10, func(in interface{}) (interface{}, error) {
			intVal := in.(int)
			return intVal * 2, nil
		})
		defer pool.Close()

		for i := 0; i < 10; i++ {
			ret, err := pool.ProcessCtx(context.Background(), 10)
			if err != nil {
				t.Fatalf("Failed to process: %v", err)
			}
			if exp, act := 20, ret.(int); exp != act {
				t.Errorf("Wrong result: %v != %v", act, exp)
			}
		}
	})

	t.Run("Returns err when ctx canceled", func(t *testing.T) {
		pool := NewFunc(1, func(in interface{}) (interface{}, error) {
			intVal := in.(int)
			<-time.After(time.Millisecond * 100)
			return intVal * 2, nil
		})
		defer pool.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		
		_, act := pool.ProcessCtx(ctx, 10)
		if exp := context.DeadlineExceeded; exp != act {
			t.Errorf("Wrong error returned: %v != %v", act, exp)
		}
	})
}

func TestCallbackJob(t *testing.T) {
	pool := NewCallback[interface{}, interface{}](10)
	defer pool.Close()

	var counter int32
	for i := 0; i < 10; i++ {
		ret, err := pool.Process(func() {
			atomic.AddInt32(&counter, 1)
		})
		if err != nil {
			t.Errorf("Should not got error: %d %v", i, err)
		}
		if ret != nil {
			t.Errorf("Non-nil callback response: %v", ret)
		}
	}

	_, err := pool.Process("foo")
	if exp, act := ErrJobNotFunc, err; exp != act {
		t.Errorf("Wrong result from non-func: %v != %v", act, exp)
	}

	if exp, act := int32(10), counter; exp != act {
		t.Errorf("Wrong result: %v != %v", act, exp)
	}
}

func TestTimeout(t *testing.T) {
	pool := NewFunc(1, func(in interface{}) (interface{}, error) {
		intVal := in.(int)
		<-time.After(time.Millisecond)
		return intVal * 2, nil
	})
	defer pool.Close()

	_, act := pool.ProcessTimed(10, time.Duration(1))
	if exp := ErrJobTimedOut; exp != act {
		t.Errorf("Wrong error returned: %v != %v", act, exp)
	}
}

func TestTimedJobsAfterClose(t *testing.T) {
	pool := NewFunc(1, func(in interface{}) (interface{}, error) {
		return 1, nil
	})
	pool.Close()

	_, act := pool.ProcessTimed(10, time.Duration(10*time.Millisecond))
	if exp := ErrPoolNotRunning; exp != act {
		t.Errorf("Wrong error returned: %v != %v", act, exp)
	}
}

func TestJobsAfterClose(t *testing.T) {
	pool := NewFunc(1, func(in interface{}) (interface{}, error) {
		return 1, nil
	})
	pool.Close()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Process after Stop() did not panic")
		}
	}()

	pool.Process(10)
}

func TestParallelJobs(t *testing.T) {
	nWorkers := 10

	jobGroup := sync.WaitGroup{}
	testGroup := sync.WaitGroup{}

	pool := NewFunc(nWorkers, func(in interface{}) (interface{}, error) {
		jobGroup.Done()
		jobGroup.Wait()

		intVal := in.(int)
		return intVal * 2, nil
	})
	defer pool.Close()

	for j := 0; j < 1; j++ {
		jobGroup.Add(nWorkers)
		testGroup.Add(nWorkers)

		for i := 0; i < nWorkers; i++ {
			go func() {
				ret, err := pool.Process(10)
				if err != nil {
					t.Errorf("Should not got error: %v", err)
				}
				if exp, act := 20, ret.(int); exp != act {
					t.Errorf("Wrong result: %v != %v", act, exp)
				}
				testGroup.Done()
			}()
		}

		testGroup.Wait()
	}
}

//------------------------------------------------------------------------------

type mockWorker struct {
	blockProcChan  chan struct{}
	blockReadyChan chan struct{}
	interruptChan  chan struct{}
	terminated     bool
}

func (m *mockWorker) Process(in interface{}) (interface{}, error) {
	select {
	case <-m.blockProcChan:
	case <-m.interruptChan:
	}
	return in, nil
}

func (m *mockWorker) BlockUntilReady() {
	<-m.blockReadyChan
}

func (m *mockWorker) Interrupt() {
	m.interruptChan <- struct{}{}
}

func (m *mockWorker) Terminate() {
	m.terminated = true
}

func TestCustomWorker(t *testing.T) {
	pool := New(1, func() Worker[interface{}, interface{}] {
		return &mockWorker{
			blockProcChan:  make(chan struct{}),
			blockReadyChan: make(chan struct{}),
			interruptChan:  make(chan struct{}),
		}
	})

	worker1, ok := pool.workers[0].worker.(*mockWorker)
	if !ok {
		t.Fatal("Wrong type of worker in pool")
	}

	if worker1.terminated {
		t.Fatal("Worker started off terminated")
	}

	_, err := pool.ProcessTimed(10, time.Millisecond)
	if exp, act := ErrJobTimedOut, err; exp != act {
		t.Errorf("Wrong error: %v != %v", act, exp)
	}

	close(worker1.blockReadyChan)
	_, err = pool.ProcessTimed(10, time.Millisecond)
	if exp, act := ErrJobTimedOut, err; exp != act {
		t.Errorf("Wrong error: %v != %v", act, exp)
	}

	close(worker1.blockProcChan)
	v, err := pool.Process(10)
	if err != nil {
		t.Errorf("Should not got error: %v", err)
	}
	if exp, act := 10, v.(int); exp != act {
		t.Errorf("Wrong result: %v != %v", act, exp)
	}

	pool.Close()
	if !worker1.terminated {
		t.Fatal("Worker was not terminated")
	}
}

//------------------------------------------------------------------------------

func BenchmarkFuncJob(b *testing.B) {
	pool := NewFunc(10, func(in interface{})(interface{}, error) {
		intVal := in.(int)
		return intVal * 2, nil
	})
	defer pool.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ret, err := pool.Process(10)
		if err != nil {
			b.Errorf("Should not got error: %v", err)
		}
		if exp, act := 20, ret.(int); exp != act {
			b.Errorf("Wrong result: %v != %v", act, exp)
		}
	}
}

func BenchmarkFuncTimedJob(b *testing.B) {
	pool := NewFunc(10, func(in interface{}) (interface{}, error) {
		intVal := in.(int)
		return intVal * 2, nil
	})
	defer pool.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ret, err := pool.ProcessTimed(10, time.Second)
		if err != nil {
			b.Error(err)
		}
		if exp, act := 20, ret.(int); exp != act {
			b.Errorf("Wrong result: %v != %v", act, exp)
		}
	}
}

//------------------------------------------------------------------------------
