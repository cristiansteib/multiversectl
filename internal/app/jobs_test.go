package app_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func TestJobsRunTogetherUpToTheLimitAndReportEachOutcomeInOrder(t *testing.T) {
	var running, peak atomic.Int32
	boom := errors.New("boom")
	var jobs []app.Job
	for i := 0; i < 6; i++ {
		i := i
		jobs = append(jobs, app.Job{Name: string(rune('a' + i)), Run: func(context.Context) error {
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			running.Add(-1)
			if i == 2 {
				return boom
			}
			return nil
		}})
	}
	errs := app.RunJobs(context.Background(), 3, jobs, nil)
	if peak.Load() != 3 {
		t.Errorf("peak concurrency = %d, want 3", peak.Load())
	}
	for i, err := range errs {
		if (i == 2) != errors.Is(err, boom) {
			t.Errorf("job %d: err = %v", i, err)
		}
	}
}

func TestACancelledRunStartsNothingMore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran atomic.Int32
	errs := app.RunJobs(ctx, 2, []app.Job{{Name: "x", Run: func(context.Context) error { ran.Add(1); return nil }}}, nil)
	if ran.Load() != 0 || !errors.Is(errs[0], context.Canceled) {
		t.Errorf("ran=%d errs=%v", ran.Load(), errs)
	}
}
