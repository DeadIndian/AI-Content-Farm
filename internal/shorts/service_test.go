package shorts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidation(t *testing.T) {
	for _, raw := range []string{"https://youtube.com.evil.org/watch?v=1", "file:///tmp/video", "https://youtube.com/playlist?list=1", "https://evil.org/youtube.com/watch?v=1"} {
		req := Request{URL: raw}
		if Validate(&req) == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	req := Request{URL: "youtu.be/abc"}
	if err := Validate(&req); err != nil {
		t.Fatal(err)
	}
	if req.MaxDuration != 45 || req.Layout != "fit" {
		t.Fatal("incorrect defaults")
	}
	req.MaxDuration = 46
	if Validate(&req) == nil {
		t.Fatal("accepted >45 seconds")
	}
}

func TestSourceCannotEscapeLibrary(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.mp4")); err != nil {
		t.Fatal(err)
	}
	if _, err := localSource(root, "link.mp4"); err == nil {
		t.Fatal("accepted symlink outside library")
	}
	if _, err := localSource(root, outside); err == nil {
		t.Fatal("accepted absolute source")
	}
}

func TestPersistCancelAndRestart(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Keep the worker stopped while exercising durable queue state.
	s, err := New(ctx, filepath.Join(root, "jobs.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Create(Request{URL: "https://youtu.be/example"}, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	j.Status = "completed"
	if err := s.update(j); err == nil {
		t.Fatal("worker overwrote cancellation")
	}
	s.Close()
	s, err = New(ctx, filepath.Join(root, "jobs.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "cancelled" || loaded.OutputDir != root {
		t.Fatalf("job not persisted: %+v", loaded)
	}
}
