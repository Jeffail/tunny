![Tunny](http://www.creepybit.co.uk/images/tunny_logo_small.png?v=3 "Tunny")

Tunny is a golang library for creating and managing a goroutine pool, aiming to be simple, intuitive, ground breaking, revolutionary, world dominating and stuff.

Use cases for tunny are any situation where a large and constant flood of jobs are imminent at an indeterminate rate and you need to bottleneck those jobs through a fixed number of dedicated worker goroutines. A common use case is to limit the hard work done in your software to the number of CPU's available, preventing threads from foolishly competing with each other for CPU time.

Tunny works internally by allowing workers to pick up jobs ad-hoc rather than assigning each their own queue. There are many advantages to this method but it helps the most when the jobs vary greatly in processing time (like language detection or sentiment analysis).

https://godoc.org/github.com/Jeffail/tunny

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
	numCPUs := runtime.NumCPU()
	numJobs := len(inputs)
	outputs := make([]float64, numJobs)

	wg := new(sync.WaitGroup)
	wg.Add(numJobs)

	runtime.GOMAXPROCS(numCPUs)

	/* Create the pool, and specify the job each worker should perform,
	 * if each worker needs to carry its own state then this can also
	 * be accomplished, read on.
	 */
	pool, _ := tunny.CreatePool(numCPUs, func(object interface{}) interface{} {

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

		}(i)
	}

	// Wait for all jobs to be completed before closing the pool
	wg.Wait()

	return outputs
}

...
```

This particular example, since it all resides in the one func, could actually be done with less code by simply spawning numCPU's goroutines that gobble up a shared channel of float64's. This would probably also be quicker since you waste cycles here boxing and unboxing the job values, but the example serves well as a demonstration of the API.

##So where do I actually benefit from using tunny?

Imagine a situation where at the front end you have a source of jobs that arrive at a non-constant and unpredictable rate, and for each job the back end must perform a large calculation of varying difficulty and respond with the result. You want to connect the front end with the back in a way that doesn't flood your machine with competing work threads, and also where each job can be received and responded to synchronously in a simple way.

These situations are where Tunny will help the most. To illustrate, let's imagine that func ServeThisClient(client NetworkClient, request NetworkRequest) is an asynchronous call instigated by your network handler to serve each individual network request.

```go
...

var pool, _ = tunny.CreatePool(runtime.NumCPU(), func(object interface{}) interface{} {

	return ThisCallDoesTonsOfHardWork(object)

}).Open()

// Each call will be from a fresh goroutine
func ServeThisClient(client NetworkClient, request NetworkRequest) {

	// Time out after 500ms, this is explained with further examples below
	result, err := pool.SendWorkTimed(500, request.Body)
	if err == nil {
		client.SendResponse(result)
	} else {
		client.SendErrorResponse(err)
	}

}

...
```

Not only will this example ensure that the hard work is only done across as many goroutines as you have CPU cores, but because a time out is specified then the process is guaranteed not to hang and explode with infinite waiting requests when the number of requests surpasses the resources available, as long as your server can handle 500ms worth of requests in memory.

##Can I send a closure instead of data?

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
func (worker *customWorker) TunnyReady() bool {
	return true
}

// This is where the work actually happens
func (worker *customWorker) TunnyJob(data interface{}) interface{} {
	/* TODO: Use and modify state
	 * there's no need for thread safety paradigms here unless the
	 * data is being accessed from another goroutine outside of
	 * the pool.
	 */
	if outputStr, ok := data.(string); ok {
		return ("custom job done: " + outputStr)
	}
	return nil
}

func TestCustomWorkers (t *testing.T) {
	outChan := make(chan int, 10)

	wg := new(sync.WaitGroup)
	wg.Add(10)

	workers := make([]tunny.TunnyWorker, 4)
	for i, _ := range workers {
		workers[i] = &(customWorker{})
	}

	pool, _ := tunny.CreateCustomPool(workers).Open()

	defer pool.Close()

	for i := 0; i < 10; i++ {
		go func() {
			value, _ := pool.SendWork("hello world")
			fmt.Println(value.(string))

			wg.Done()
		}()
	}

	wg.Wait()
}

...
```

You'll notice that as well as the important TunnyJob(data interface{}) interface{} call to implement there is also the call TunnyReady() bool. TunnyReady is potentially an important part of the TunnyWorker that allows you to use your state to determine whether or not this worker should take on another job, and answer true or false accordingly.

For example, your worker could hold a counter of how many jobs it has done, and perhaps after a certain amount it should perform another act before taking on more work, it's important to use TunnyReady for these occasions since blocking the TunnyJob call will hold up the waiting client.

It is recommended that you do not block TunnyReady() whilst you wait for some condition to change, since this can prevent the pool from closing the worker goroutines. Currently, TunnyReady is called at 5 millisecond intervals until you answer true or the pool is closed.

##I need more control

You crazy fool, let's take this up to the next level. You can optionally implement these methods...

```go
...

/* This is called on each worker when pool.Open is activated, jobs will begin to
 * arrive afterwards.
 */
func (worker *customWorker) TunnyInitialize() {
	// Do stuff
}

/* This is called on each worker when pool.Close is activated, jobs will have
 * already stopped by this point. Use it as an opportunity to clean yourself up.
 */
func (worker *customWorker) TunnyTerminate() {
	// Undo stuff
}

...
```

##Can a worker detect when a timeout occurs?

Yes, you can optionally implement the following method on your worker:

```go
...

func (worker *interruptableWorker) TunnyInterrupt() {

	/* This is called from a separate goroutine, so only use thread safe
	 * methods to communicate with your worker.

	 * Something like this can be used to indicate midway through a job
	 * that it should be abandoned, in your TunnyJob call you can simply
	 * return nil.
	 */
	worker.stopChan<-1

}

...
```

This method will be called in the event that a timeout occurs whilst waiting for the result. TunnyInterrupt is called from a newly spawned goroutine, whose job is to call TunnyInterrupt and then follow it by receiving the eventual output from the worker thread.

You can therefore know for certain that throughout the call the worker thread will not have received the next job. Infact, you can verify this yourself by ensuring that TunnyReady() is not called before this method exits.

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
	DoThatOtherThing(result, err)

})

...
```

And another, because why not?

```go
...

outputChan := make(chan string)

pool, _ := tunny.CreatePool(4, func(data interface{}) interface{} {

	outputChan <-doHardJobOrWhatever(data)
	return nil // Not bothered about synchronous output

}).Open();

for i := 0; i < 100; i++ {
	pool.SendWorkAsync(inputs[i], nil)
}

pool.SendWorkAsync("one last job", nil)

// Do other stuff here

...
```

##Behaviours and caveats:

###- Workers request jobs on an ad-hoc basis

When there is a backlog of jobs waiting to be serviced, and all workers are occupied, a job will not be assigned to a worker until it is already prepared for its next job. This means workers do not develop their own individual queues. Instead, the backlog is shared by the entire pool.

This means an individual worker is able to halt, or spend exceptional lengths of time on a single request without hindering the flow of any other requests, provided there are other active workers in the pool.

###- A job can be dropped before work is begun

Tunny has support for specified timeouts at the work request level, if this timeout is triggered whilst waiting for a worker to become available then the request is dropped entirely and no effort is wasted on the abandoned request.

###- Backlogged jobs are FIFO, for now

When a job arrives and all workers are occupied the waiting thread will lock at a select block whilst waiting to be assigned a worker. In practice this seems to create a FIFO queue, implying that this is how the implementation of golang has dealt with select blocks, channels and multiple reading goroutines.

However, I haven't found a guarantee of this behaviour in the golang documentation, so I cannot guarantee that this will always be the case.
