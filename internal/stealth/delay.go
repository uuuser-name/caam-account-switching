package stealth

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"
)

// ComputeDelay returns a random delay duration in the inclusive range
// [minSeconds, maxSeconds].
func ComputeDelay(minSeconds, maxSeconds int, rng *rand.Rand) (time.Duration, error) {
	if minSeconds < 0 {
		return 0, fmt.Errorf("minSeconds cannot be negative")
	}
	if maxSeconds < 0 {
		return 0, fmt.Errorf("maxSeconds cannot be negative")
	}
	if minSeconds > maxSeconds {
		return 0, fmt.Errorf("minSeconds cannot be greater than maxSeconds")
	}

	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	if minSeconds == maxSeconds {
		return time.Duration(minSeconds) * time.Second, nil
	}

	seconds := minSeconds + rng.Intn(maxSeconds-minSeconds+1)
	return time.Duration(seconds) * time.Second, nil
}

// WaitOptions controls stealth delay behavior.
type WaitOptions struct {
	// Output is where countdown/progress output is written.
	// If nil, output is discarded.
	Output io.Writer

	// Skip is an optional channel that, when closed, skips the remaining delay.
	Skip <-chan struct{}

	// ShowCountdown controls whether a progress bar is displayed.
	ShowCountdown bool

	// BarWidth controls the width of the progress bar (in cells).
	BarWidth int

	// Tick controls how often the countdown refreshes.
	Tick time.Duration
}

// Wait sleeps for duration unless the context is canceled or Skip fires.
// Returns (true, nil) if the delay was skipped.
func Wait(ctx context.Context, duration time.Duration, opts WaitOptions) (bool, error) {
	if duration <= 0 {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	if opts.BarWidth <= 0 {
		opts.BarWidth = 30
	}
	if opts.Tick <= 0 {
		opts.Tick = time.Second
	}

	if !opts.ShowCountdown {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-timer.C:
			return false, nil
		case <-opts.Skip:
			return true, nil
		}
	}

	start := time.Now()
	deadline := start.Add(duration)
	ticker := time.NewTicker(opts.Tick)
	defer ticker.Stop()

	render := func() {
		now := time.Now()
		elapsed := now.Sub(start)
		if elapsed < 0 {
			elapsed = 0
		}
		if elapsed > duration {
			elapsed = duration
		}

		remaining := deadline.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		seconds := int(remaining.Round(time.Second).Seconds())

		progress := float64(elapsed) / float64(duration)
		filled := int(progress * float64(opts.BarWidth))
		if filled < 0 {
			filled = 0
		}
		if filled > opts.BarWidth {
			filled = opts.BarWidth
		}

		bar := strings.Repeat("█", filled) + strings.Repeat("░", opts.BarWidth-filled)
		fmt.Fprintf(opts.Output, "\r[%s] %ds remaining (Ctrl+C to skip)", bar, seconds)
	}

	render()
	for {
		if time.Now().After(deadline) {
			fmt.Fprintln(opts.Output)
			return false, nil
		}

		select {
		case <-ctx.Done():
			fmt.Fprintln(opts.Output)
			return false, ctx.Err()
		case <-opts.Skip:
			fmt.Fprintln(opts.Output)
			return true, nil
		case <-ticker.C:
			render()
		}
	}
}
