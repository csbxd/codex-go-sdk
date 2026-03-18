package codex

import (
	"context"
	"time"
)

// RetryOnOverload retries fn only when codex app-server returns the documented
// overload JSON-RPC error.
func RetryOnOverload[T any](
	ctx context.Context,
	maxAttempts int,
	initialDelay time.Duration,
	maxDelay time.Duration,
	fn func(context.Context) (T, error),
) (T, error) {
	var zero T
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if initialDelay <= 0 {
		initialDelay = 250 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 2 * time.Second
	}

	delay := initialDelay
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		value, err := fn(ctx)
		if err == nil {
			return value, nil
		}
		if !IsOverload(err) || attempt == maxAttempts {
			return zero, err
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return zero, context.DeadlineExceeded
}
