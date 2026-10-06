package resource

import (
	"context"
	"testing"
)

func TestGateSerializesAndCancelsWaiters(t *testing.T) {
	release, err := Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx); err != context.Canceled {
		t.Fatalf("waiting work did not cancel: %v", err)
	}
	release()
	release, err = Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
