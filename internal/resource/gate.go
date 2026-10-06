// Package resource serializes heavyweight work across the Studio and Shorts queues.
package resource

import "context"

var slot = make(chan struct{}, 1)

func Acquire(ctx context.Context) (func(), error) {
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
