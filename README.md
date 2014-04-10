![Tunny](http://www.creepybit.co.uk/images/tunny_logo_small.png?v=3 "Tunny")

Tunny is a golang library for creating and managing a goroutine pool, aiming to be simple, intuitive, ground breaking, revolutionary, world dominating and stuff.

Use cases for tunny are any situation where a large and possibly constant flood of jobs are imminent but indeterminate and you need to bottleneck those jobs through a fixed number of dedicated worker goroutines. The most obvious example is as an easy wrapper for limiting the hard work done in your software to the number of CPU's available, preventing threads from foolishly competing with each other for CPU time.

Tunny helps the most when either the input jobs arrive at an inconsistent rate (from http clients, for example) or the jobs themselves vary greatly in processing time (like language detection or sentiment analysis).

##How to install:

```bash
go get github.com/jeffail/tunny
```

##How to use:

Here's an example of tunny being used to distribute a batch of calculations to a pool of workers that matches the number of CPU's, ignoring errors for simplicity:

```go
...

import (
	"github.com/jeffail/tunny"
	"sync"
)

...

func CalcRoots (inputs []float64) []float64 {
	numCPUs  := runtime.NumCPU()
	numJobs  := len(inputs)
	outputs  := make( []float64, numJobs )

	wg := new(sync.WaitGroup)
	wg.Add(numJobs)

	runtime.GOMAXPROCS(numCPUs)

	/* Create the pool, and specify the job each worker should perform,
	 * if each worker needs to carry its own state then this can also
	 * be accomplished, read on.
	 */
	pool, _ := tunny.CreatePool(numCPUs, func( object interface{} ) ( interface{} ) {

		// Hard work here
		value, _ := object.(float64)
		return math.Sqrt(value)

	}).Open()

	defer pool.Close()

	/* Creates a goroutine for all jobs, these will be blocked until
	 * a worker is available and has finished the request.
	 */
	for i := 0; i < numJobs; i++ {
		go func(index int) {

			// SendWork is thread safe and synchronous. Call it from any goroutine.
			value, _ := pool.SendWork(inputs[index])
			outputs[index], _ = value.(float64)

			wg.Done()

		}(i + 10)
	}

	// Wait for all jobs to be completed before closing the pool
	wg.Wait()

	return outputs
}

...
```

This particular example, since it all resides in the one func, could actually be done with less code by simply spawning numCPU's goroutines that gobble up a shared channel of float64's. This would probably also be quicker since you waste cycles here boxing and unboxing the job values, but the example serves well as a demonstration of the API.

##Can I specify the job for each work call?

Yes, the arguments passed to the worker are boxed as interface{}, so this can actually be a func, you can implement this yourself, or if you're not bothered about return values you can use:

```go
...

exampleChannel := make(chan int)

pool, _ := tunny.CreatePoolGeneric(numCPUs).Open()

err := pool.SendWork(func() {
	/* Do your hard work here, usual rules of closures apply here,
	 * so you can return values like so:
	 */
	exampleChannel <- 10
})

if err != nil {
	// You done goofed
}

...
```

##Specify a time out period

To make pool calls adhere to a timeout period of your choice simply swap the call to SendWork with SendWorkTimed, like so:

```go
...

// SendWorkTimed takes an argument for a timeout in milliseconds.
// If this timeout triggers the call will return with an error
if value, err := pool.SendWorkTimed(500, inputs[index]); err == nil {

	outputs[index], _ = value.(float64)

} else {
	/* A timeout most likely occured, I haven't checked this specifically because
	 * I am a lazy garbage mongler.
	 */
}

...
```

This snippet will send the job, and wait for up to 500 milliseconds for an answer. You could optionally implement a timeout yourself by starting a new goroutine that returns the output through a channel, and having that channel compete with time.After().

You'd be an idiot for doing that though because you would be forcing the pool to send work to a worker even if the timeout occured whilst waiting for a worker to become available, you muppet!

##How do I give my workers state?

The call to tunny.CreatePool will generate a pool of TunnyWorkers for you, and then assign each worker the closure argument to run for each job. You can, however, create these workers yourself, thereby allowing you to also give them their own state and methods.

Here is a short example:

```go
...

type customWorker struct {
	// TODO: Put some state here
}

// Use this call to block further jobs if necessary
func (worker *customWorker) Ready() bool {
	return true
}

// This is where the work actually happens
func (worker *customWorker) Job(data interface{}) interface{} {
	/* TODO: Use and modify state
	 * there's no need for thread safety paradigms here unless the
	 * data is being accessed from another goroutine outside of
	 * the pool.
	 */
	if outputStr, ok := data.(string); ok {
		return ("custom job done: " + outputStr )
	}
	return nil
}

func TestCustomWorkers (t *testing.T) {
	outChan  := make(chan int, 10)

	workers := make([]tunny.TunnyWorker, 4)
	for i, _ := range workers {
		workers[i] = &(customWorker{})
	}

	pool, errPool := tunny.CreateCustomPool(workers).Open()

	if errPool != nil {
		t.Errorf("Error starting pool: ", errPool)
		return
	}

	defer pool.Close()

	for i := 0; i < 10; i++ {
		go func() {
			if value, err := pool.SendWork("hello world"); err != nil {

				t.Errorf("Error returned: ", err)

			} else {

				str, _ := value.(string)
				if "custom job done: hello world" != str {
					t.Errorf("Unexpected output from custom worker")
				}

			}
			outChan <- 1
		}()
	}

	for i := 0; i < 10; i++ {
		<-outChan
	}
}

...
```

You'll notice that as well as the important Job(data interface{}) interface{} call to implement there is also the call Ready() bool. Ready is potentially an important part of the TunnyWorker that allows you to use your state to determine whether or not this worker should take on another job, and answer true or false accordingly.

For example, your worker could hold a counter of how many jobs it has done, and perhaps after a certain amount it should perform another act before taking on more work, it's important to use Ready for these occasions since blocking the Job call will hold up the waiting client.

It is recommended that you do not block Ready() whilst you wait for some condition to change, since this can prevent the pool from closing the worker goroutines. Currently, Ready is called at 5 millisecond intervals until you answer true or the pool is closed.

##I need more control

You crazy fool, let's take this up to the next level. You can optionally implement these methods...

```go
...

/* This is called on each worker when pool.Open is activated, jobs will begin to
 * arrive afterwards.
 */
func (worker *customWorker) Initialize() {
	// Do stuff
}

/* This is called on each worker when pool.Close is activated, jobs will have
 * already stopped by this point. Use it as an opportunity to clean yourself up.
 */
func (worker *customWorker) Terminate() {
	// Undo stuff
}

...
```

##Can SendWork be called asynchronously?

There are the helper functions SendWorkAsync and SendWorkTimedAsync, that are the same as their respective sync calls with an optional second argument func(interface{}, error), this is the call made when a result is returned and can be nil if there is no need for the closure.

However, if you find yourself in a situation where the sync return is not necessary then chances are you don't actually need Tunny at all. Golang is all about making concurrent programming simple by nature, and using Tunny for implementing simple async worker calls defeats the great work of the language spec and adds overhead that isn't necessary.

Here's a quick example:

```go
...

outputChan := make(chan string)

pool, _ := tunny.CreatePoolGeneric(4).Open();

for i := 0; i < 100; i++ {
	pool.SendWorkAsync(func() {

		// Work here
		outputChan <- doHardJobOrWhatever()

	}, nil) // nil here because the result is returned in the closure
}

pool.SendWorkAsync(func() {

	// Extra job here, this is executed on the worker goroutine
	DoThatThingWhatTakesLongButOnlyNeedsDoingOnce()

}, func(result interface{}, err error) {

	/* Called when the work is done, this is executed in a new goroutine.
	 * Use this call to forward the result onwards without blocking the worker.
	 */
	DoThatOtherThing()

})

...
```

##So where do I actually benefit from using tunny?

You don't, I'm not a god damn charity.

##Behaviours and caveats:

###- Workers request jobs on an ad-hoc basis

When there is a backlog of jobs waiting to be serviced, and all workers are occupied, a job will not be assigned to a worker until it is already prepared for its next job. This means workers do not develop their own individual queues. Instead, the backlog is shared by the entire pool.

This means an individual worker is able to halt, or spend exceptional lengths of time on a single request without hindering the flow of any other requests, provided there are other active workers in the pool.

###- A job can be dropped before work is begun

Tunny has support for specified timeouts at the work request level, if this timeout is triggered whilst waiting for a worker to become available then the request is dropped entirely and no effort is wasted on the abandoned request.

###- A job request is unprioritized

The incoming requests are not prioritized in any way, if the current work load is beyond the capacity of the pool then the remaining jobs are selected at random. This is intentional because a pool with an increasing backlog and an orderly queue of jobs would favour requests that are closest to reaching a potential timeout. If all jobs are assumed to be of equal priority then the system suffers when favour is granted to the jobs most likely to be discarded before completion.

The plan is to have this behaviour configurable, and interchangeble with an orderly queue.
