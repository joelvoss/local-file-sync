package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunParallel_NoTasks verifies that running with no tasks is a no-op and
// returns no error.
func TestRunParallel_NoTasks(t *testing.T) {
	if err := RunParallel(context.Background(), 4, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRunParallel_AllSuccess verifies that all tasks complete successfully when
// no errors occur.
func TestRunParallel_AllSuccess(t *testing.T) {
	var count atomic.Int32
	tasks := []Task{}
	for range 10 {
		tasks = append(tasks, func(ctx context.Context) error {
			time.Sleep(5 * time.Millisecond)
			count.Add(1)
			return nil
		})
	}
	if err := RunParallel(context.Background(), 3, tasks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count.Load() != int32(len(tasks)) {
		t.Fatalf("expected %d executions got %d", len(tasks), count.Load())
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRunParallel_FirstErrorStopsOthers verifies that if one task returns an
// error, other tasks are stopped as soon as possible and that error is
// propagated.
func TestRunParallel_FirstErrorStopsOthers(t *testing.T) {
	var ran atomic.Int32
	errSentinel := errors.New("boom")
	tasks := []Task{}
	for i := range 50 {
		tasks = append(tasks, func(ctx context.Context) error {
			// NOTE(joel): Introduce an error early for i == 5
			if i == 5 {
				return errSentinel
			}
			// NOTE(joel): Simulate work
			time.Sleep(2 * time.Millisecond)
			ran.Add(1)
			return nil
		})
	}
	err := RunParallel(context.Background(), 8, tasks)
	if !errors.Is(err, errSentinel) {
		t.Fatalf("expected sentinel error; got %v", err)
	}
	// NOTE(joel): We can't assert an exact upper bound reliably, but ran should
	// be < len(tasks)-1
	if ran.Load() >= int32(len(tasks)-1) {
		t.Fatalf("expected early stop; ran=%d total=%d", ran.Load(), len(tasks))
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRunParallel_AutoConcurrency verifies that using concurrency <=0 triggers
// automatic concurrency determination and that all tasks complete.
func TestRunParallel_AutoConcurrency(t *testing.T) {
	// NOTE(joel): Use concurrency <=0 to trigger auto behavior.
	var count atomic.Int32
	tasks := []Task{}
	for range 5 {
		tasks = append(tasks, func(ctx context.Context) error { count.Add(1); return nil })
	}
	if err := RunParallel(context.Background(), 0, tasks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count.Load() != 5 {
		t.Fatalf("expected 5 tasks executed, got %d", count.Load())
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRunParallel_ContextCancellationPropagation verifies that if the parent
// context is cancelled, tasks observe that cancellation and stop as soon as
// possible.
func TestRunParallel_ContextCancellationPropagation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	tasks := []Task{}
	startedFirst := make(chan struct{})
	firstSawCancel := make(chan struct{})

	// NOTE(joel): First task waits on cancellation to complete.
	tasks = append(tasks, func(ctx context.Context) error {
		close(startedFirst)
		<-ctx.Done()
		close(firstSawCancel)
		return nil
	})

	// NOTE(joel): Additional tasks just increment a counter and sleep a little
	// so the scheduler has time to start some of them before cancellation. We
	// only assert that not all tasks necessarily complete after cancellation is
	// requested, which is a weaker (but correct) guarantee for this worker
	// design where jobs are eagerly queued.
	var ran atomic.Int32
	for i := 0; i < 10; i++ {
		tasks = append(tasks, func(ctx context.Context) error {
			// NOTE(joel): simulate brief work
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Millisecond):
			}
			ran.Add(1)
			return nil
		})
	}

	go func() {
		<-startedFirst
		cancel()
	}()

	_ = RunParallel(parent, 2, tasks)

	select {
	case <-firstSawCancel:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first task did not observe cancellation")
	}

	// NOTE(joel): We can't require zero (other workers may have already pulled
	// jobs), but it should be less than the total submitted auxiliary tasks.
	if ran.Load() >= 10 {
		t.Fatalf("expected some tasks to be prevented by cancellation; ran=%d", ran.Load())
	}
}
