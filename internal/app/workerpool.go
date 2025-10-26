package app

import (
	"context"
	"runtime"
	"sync"
)

// Task represents a unit of work executed by a worker pool.
type Task func(ctx context.Context) error

////////////////////////////////////////////////////////////////////////////////

// RunParallel executes tasks in parallel with up to concurrency workers.
// If concurrency <=0 an automatic value based on NumCPU (capped between 2 and
// 8) is used. The returned error is the first non-nil error encountered
// (others may be suppressed).
func RunParallel(parentCtx context.Context, concurrency int, tasks []Task) error {
	if len(tasks) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = max(min(runtime.NumCPU(), 8), 2)
	}
	if concurrency > len(tasks) {
		concurrency = len(tasks)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	type job struct{ idx int }
	jobs := make(chan job)
	errCh := make(chan error, concurrency)
	wg := sync.WaitGroup{}

	// NOTE(joel): Worker goroutine to process jobs from the channel. Each
	// task creates a new job with its index in the tasks slice.
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			if ctx.Err() != nil {
				return
			}
			// NOTE(joel): Run task and report first error. Cancel context to stop
			// other workers from executing new tasks.
			if err := tasks[j.idx](ctx); err != nil {
				select {
				case errCh <- err:
					cancel()
				default:
				}
				return
			}
		}
	}

	// NOTE(joel): Start workers and feed jobs.
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go worker()
	}
	for i := range tasks {
		if ctx.Err() != nil {
			break
		}
		jobs <- job{idx: i}
	}
	close(jobs)

	// NOTE(joel): Wait for workers to finish and close error channel.
	// If any errors were reported, return the first one.
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			return e
		}
	}
	return nil
}
