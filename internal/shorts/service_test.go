package shorts

import (
	"context"
	"math"
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

func TestEditOptions(t *testing.T) {
	for _, duration := range []float64{5, 30, 40, 45} {
		req := Request{URL: "https://youtu.be/example", MaxDuration: duration, CutMode: "fixed", EditStyle: "reel"}
		if err := Validate(&req); err != nil {
			t.Fatal(err)
		}
	}
	for _, req := range []Request{
		{CutMode: "unknown"}, {EditStyle: "unknown"}, {MaxDuration: math.NaN()},
		{ClipStart: pointer(-1)}, {ClipStart: pointer(math.Inf(1))},
	} {
		req.URL = "https://youtu.be/example"
		if Validate(&req) == nil {
			t.Fatalf("accepted invalid edit: %+v", req)
		}
	}
}

func pointer(value float64) *float64 { return &value }

func TestRegeneratePreservesOriginalAndCacheLineage(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := New(ctx, filepath.Join(root, "jobs.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	original, err := s.Create(Request{URL: "https://youtu.be/example"}, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Regenerate(original.ID, original.Request); err == nil {
		t.Fatal("accepted an active parent")
	}
	original.Status, original.SourceDuration = "completed", 7200
	original.Clips = []Clip{{Filename: "old.mp4"}}
	if err := s.save(original); err != nil {
		t.Fatal(err)
	}
	options := Request{URL: "https://evil.example/override", MaxDuration: 40, EditStyle: "reel", CutMode: "fixed", ClipStart: pointer(60)}
	edited, err := s.Regenerate(original.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID == original.ID || edited.SourceJobID != original.ID || edited.Request.URL != original.Request.URL || len(edited.Clips) != 0 {
		t.Fatalf("bad edit: %+v", edited)
	}
	edited.Status = "completed"
	if err := s.save(edited); err != nil {
		t.Fatal(err)
	}
	batch, err := s.Regenerate(edited.ID, Request{MaxDuration: 30})
	if err != nil {
		t.Fatal(err)
	}
	if batch.SourceJobID != original.ID || batch.Request.ClipStart != nil {
		t.Fatalf("bad batch lineage: %+v", batch)
	}
	loaded, err := s.Get(original.ID)
	if err != nil || loaded.Status != "completed" || len(loaded.Clips) != 1 || loaded.Request.MaxDuration != 45 {
		t.Fatalf("original changed: %+v, %v", loaded, err)
	}
	options.ClipStart = pointer(7200)
	if _, err := s.Regenerate(original.ID, options); err == nil {
		t.Fatal("accepted start beyond source")
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
