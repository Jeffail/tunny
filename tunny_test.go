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
	"testing"
	"time"
	"runtime"
)

/* time.Sleep seems to be spawning a goroutine that doesn't stop. This is fine and stuff
 * but tunny tests do a comparisson of runtime.NumGoroutine before and after to make sure
 * that goroutines aren't being left open after .Close(), so this test simply opens that
 * goroutine beforehand.
 */
func TestTestTestyTestAllTheTestsNeedTesting(t * testing.T) {
	time.Sleep(time.Millisecond)
}

func TestTimeout(t *testing.T) {
	outChan  := make(chan int, 3)
	routines := runtime.NumGoroutine()

	pool, errPool := CreatePool(1, func(object interface{}) interface{} {
		time.Sleep(500 * time.Millisecond)
		return nil
	}).Open()

	if errPool != nil {
		t.Errorf("Error starting pool: ", errPool)
		return
	}

	before := time.Now()

	go func() {
		if _, err := pool.SendWorkTimed(200, nil); err == nil {
			t.Errorf("No timeout triggered thread one")
		} else {
			taken := ( time.Since(before) / time.Millisecond )
			if taken > 210 {
				t.Errorf("Time taken at thread one: ", taken, ", with error: ", err)
			}
		}
		outChan <- 1

		go func() {
			if _, err := pool.SendWork(nil); err == nil {
			} else {
				t.Errorf("Error at thread three: ", err)
			}
			outChan <- 1
		}()
	}()

	go func() {
		if _, err := pool.SendWorkTimed(200, nil); err == nil {
			t.Errorf("No timeout triggered thread two")
		} else {
			taken := ( time.Since(before) / time.Millisecond )
			if taken > 210 {
				t.Errorf("Time taken at thread two: ", taken, ", with error: ", err)
			}
		}
		outChan <- 1
	}()

	for i := 0; i < 3; i++ {
		<-outChan
	}

	pool.Close()

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

func TestTimeoutRequests(t *testing.T) {
	n_polls  := 200
	outChan  := make(chan int, n_polls)
	routines := runtime.NumGoroutine()

	pool, errPool := CreatePool(1, func(object interface{}) interface{} {
		time.Sleep(time.Millisecond)
		return nil
	}).Open()

	if errPool != nil {
		t.Errorf("Error starting pool: ", errPool)
		return
	}

	for i := 0; i < n_polls; i++ {
		if _, err := pool.SendWorkTimed(50, nil); err == nil {
		} else {
			t.Errorf("thread %v error: ", i, err)
		}
		outChan <- 1
	}

	for i := 0; i < n_polls; i++ {
		<-outChan
	}

	pool.Close()

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

func validateReturnInt(t *testing.T, expecting int, object interface{}) {
	if w, ok := object.(int); ok {
		if w != expecting {
			t.Errorf("Wrong, expected %v, got %v", expecting, w)
		}
	} else {
		t.Errorf("Wrong, expected int")
	}
}

func TestBasic(t *testing.T) {
	sizePool, repeats, sleepFor, margin := 16, 2, 250, 100
	outChan  := make(chan int, sizePool)
	routines := runtime.NumGoroutine()

	runtime.GOMAXPROCS(runtime.NumCPU())

	pool, errPool := CreatePool(sizePool, func(object interface{}) interface{} {
		time.Sleep(time.Duration(sleepFor) * time.Millisecond)
		if w, ok := object.(int); ok {
			return w * 2
		}
		return "Not an int!"
	}).Open()

	if errPool != nil {
		t.Errorf("Error starting pool: ", errPool)
		return
	}

	for i := 0; i < sizePool * repeats; i++ {
		go func() {
			if out, err := pool.SendWork(50); err == nil {
				validateReturnInt (t, 100, out)
			} else {
				t.Errorf("Error returned: ", err)
			}
			outChan <- 1
		}()
	}

	before := time.Now()

	for i := 0; i < sizePool * repeats; i++ {
		<-outChan
	}

	taken    := float64( time.Since(before) ) / float64(time.Millisecond)
	expected := float64( sleepFor + margin ) * float64(repeats)

	if taken > expected {
		t.Errorf("Wrong, should have taken less than %v seconds, actually took %v", expected, taken)
	}

	pool.Close()

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

func TestGeneric(t *testing.T) {
	routines := runtime.NumGoroutine()
	runtime.GOMAXPROCS(runtime.NumCPU())

	if pool, err := CreatePoolGeneric(10).Open(); err == nil {
		outChan  := make(chan int, 10)

		for i := 0; i < 10; i++ {
			go func(id int) {
				one, err := pool.SendWork(func() {
					outChan <- id
				})

				if err != nil {
					t.Errorf("Generic call timed out!")
				}

				if one != nil {
					if funcerr, ok := one.(error); ok {
						t.Errorf("Generic worker call: ", funcerr)
					} else {
						t.Errorf("Unexpected result from generic worker")
					}
				}
			}(i)
		}

		results := make([]int, 10)

		for i := 0; i < 10; i++ {
			value := <-outChan
			if results[value] != 0 || value > 9 || value < 0 {
				t.Errorf("duplicate or incorrect key: %v", value)
			}
			results[value] = 1
		}

		pool.Close()

	} else {
		t.Errorf("Error starting pool: ", err)
		return
	}

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

func TestExampleCase(t *testing.T) {
	outChan  := make(chan int, 10)
	routines := runtime.NumGoroutine()
	runtime.GOMAXPROCS(runtime.NumCPU())

	pool, errPool := CreatePool(4, func(object interface{}) interface{} {
		if str, ok := object.(string); ok {
			return "job done: " + str
		}
		return nil
	}).Open()

	if errPool != nil {
		t.Errorf("Error starting pool: ", errPool)
		return
	}

	for i := 0; i < 10; i++ {
		go func() {
			if value, err := pool.SendWork("hello world"); err == nil {
				if _, ok := value.(string); ok {
				} else {
					t.Errorf("Not a string!")
				}
			} else {
				t.Errorf("Error returned: ", err)
			}
			outChan <- 1
		}()
	}

	for i := 0; i < 10; i++ {
		<-outChan
	}

	pool.Close()

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

type customWorker struct {
	jobsCompleted int
}

func (worker *customWorker) Ready() bool {
	return true
}

func (worker *customWorker) Job(data interface{}) interface{} {
	/* There's no need for thread safety paradigms here unless the data is being accessed from
	 * another goroutine outside of the pool.
	 */
	if outputStr, ok := data.(string); ok {
		(*worker).jobsCompleted++;
		return ("custom job done: " + outputStr)
	}
	return nil
}

func TestCustomWorkers(t *testing.T) {
	outChan  := make(chan int, 10)
	routines := runtime.NumGoroutine()

	runtime.GOMAXPROCS(runtime.NumCPU())

	workers := make([]TunnyWorker, 4)
	for i, _ := range workers {
		workers[i] = &(customWorker{ jobsCompleted: 0 })
	}

	pool, errPool := CreateCustomPool(workers).Open()

	if errPool != nil {
		t.Errorf("Error starting pool: ", errPool)
		return
	}

	for i := 0; i < 10; i++ {
		/* Calling SendWork is thread safe, go ahead and call it from any goroutine.
		 * The call will block until a worker is ready and has completed the job.
		 */
		go func() {
			if value, err := pool.SendWork("hello world"); err == nil {
				if str, ok := value.(string); ok {
					if str != "custom job done: hello world" {
						t.Errorf("Unexpected output from custom worker")
					}
				} else {
					t.Errorf("Not a string!")
				}
			} else {
				t.Errorf("Error returned: ", err)
			}
			outChan <- 1
		}()
	}

	for i := 0; i < 10; i++ {
		<-outChan
	}

	/* After this call we should be able to guarantee that no other go routine is
	 * accessing the workers.
	 */
	pool.Close()

	totalJobs := 0
	for i := 0; i < len(workers); i++ {
		if custom, ok := workers[i].(*customWorker); ok {
			totalJobs += (*custom).jobsCompleted
		} else {
			t.Errorf("could not cast to customWorker")
		}
	}

	if totalJobs != 10 {
		t.Errorf("Total jobs expected: %v, actual: %v", 10, totalJobs)
	}

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

type customExtendedWorker struct {
	jobsCompleted int
	asleep        bool
}

func (worker *customExtendedWorker) Job(data interface{}) interface{} {
	if outputStr, ok := data.(string); ok {
		(*worker).jobsCompleted++;
		return ("custom job done: " + outputStr)
	}
	return nil
}

// Do 10 jobs and then stop.
func (worker *customExtendedWorker) Ready() bool {
	return !(*worker).asleep && ((*worker).jobsCompleted < 10)
}

func (worker *customExtendedWorker) Initialize() {
	(*worker).asleep = false
}

func (worker *customExtendedWorker) Terminate() {
	(*worker).asleep = true
}

func TestCustomExtendedWorkers(t *testing.T) {
	outChan  := make(chan int, 10)
	routines := runtime.NumGoroutine()
	runtime.GOMAXPROCS(runtime.NumCPU())

	extWorkers   := make([]*customExtendedWorker, 4)
	tunnyWorkers := make([]TunnyWorker, 4)
	for i, _ := range tunnyWorkers {
		extWorkers  [i] = &(customExtendedWorker{ jobsCompleted: 0, asleep: true })
		tunnyWorkers[i] = extWorkers[i]
	}

	pool := CreateCustomPool(tunnyWorkers);

	for j := 0; j < 1; j++ {

		_, errPool := pool.Open()

		for i, _ := range extWorkers {
			if (*extWorkers[i]).asleep {
				t.Errorf("Worker is still asleep!")
			}
		}

		if errPool != nil {
			t.Errorf("Error starting pool: ", errPool)
			return
		}

		for i := 0; i < 40; i++ {
			/* Calling SendWork is thread safe, go ahead and call it from any goroutine.
			 * The call will block until a worker is ready and has completed the job.
			 */
			go func() {
				if value, err := pool.SendWork("hello world"); err == nil {
					if str, ok := value.(string); ok {
						if str != "custom job done: hello world" {
							t.Errorf("Unexpected output from custom worker")
						}
					} else {
						t.Errorf("Not a string!")
					}
				} else {
					t.Errorf("Error returned: ", err)
				}
				outChan <- 1
			}()
		}

		for i := 0; i < 40; i++ {
			<-outChan
		}

		/* After this call we should be able to guarantee that no other go routine is
		 * accessing the workers.
		 */
		pool.Close()

		expectedJobs := ((j + 1) * 10)
		for i, _ := range extWorkers {
			if (*extWorkers[i]).jobsCompleted != expectedJobs {
				t.Errorf( "Expected %v jobs completed, actually: %v",
					expectedJobs,
					(*extWorkers[i]).jobsCompleted,
				)
			}
			if !(*extWorkers[i]).asleep {
				t.Errorf("Worker is still awake!")
			}
		}
	}

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

func TestAsyncCalls(t *testing.T) {
	routines := runtime.NumGoroutine()

	numWorkers, numData := 4, 400
	outChan := make(chan int, numData)

	pool, err := CreatePool(numWorkers, func(data interface{}) interface{} {
		if intData, ok := data.(int); ok {
			time.Sleep(time.Millisecond * 5)
			outChan <- intData
			return intData
		} else {
			t.Errorf("Not and int!")
		}
		return nil
	}).Open()

	if err != nil {
		t.Errorf("Error starting pool: ", err)
		return
	}

	for i := 0; i < numData; i++ {
		pool.SendWorkAsync(i, nil)
	}

	for i := 0; i < numData; i++ {
		<-outChan
	}

	pool.Close()

	pool.Open()

	for i := 0; i < numData; i++ {
		pool.SendWorkAsync(i, func(val interface{}, workErr error) {
			if workErr != nil {
				t.Errorf("Error: %v, ", workErr)
			}
			if _, ok := val.(int); !ok {
				t.Errorf("Not and int!")
			}
		})
	}

	for i := 0; i < numData; i++ {
		<-outChan
	}

	pool.Close()

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}

/*

Test template

func Test(t *testing.T) {
	routines := runtime.NumGoroutine()

	pool, poolErr := CreatePool(numWorkers, func(data interface{}) interface{} {
	}).Open()

	if poolErr != nil {
		t.Errorf("Error starting pool: ", poolErr)
		return
	}

	// TEST HERE

	pool.Close()

	if routines != runtime.NumGoroutine() {
		t.Errorf("Excess goroutines: %v", runtime.NumGoroutine() - routines)
	}
}
*/
