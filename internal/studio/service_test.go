package studio

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gollabharath/ai-content-farm/internal/resource"
)

func exampleRequest() Request {
	return Request{Quality: "preview", Draft: Draft{Title: "Example", Topic: "An example", Pair: "nova-atlas", Format: "portrait", Provider: "manual",
		Scenes: []Scene{{Speaker: 0, Text: "First presenter explains the subject.", Visual: "THE SUBJECT"}, {Speaker: 1, Text: "Second presenter gives a useful example.", Visual: "AN EXAMPLE"}}}}
}

func TestValidationRejectsUnsafeAndIncompleteDrafts(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"unknown pair", func(r *Request) { r.Draft.Pair = "../../x" }},
		{"missing speaker", func(r *Request) { r.Draft.Scenes[1].Speaker = 0 }},
		{"unsafe source", func(r *Request) { r.Draft.Sources = []Source{{URL: "javascript:alert(1)"}} }},
		{"empty text", func(r *Request) { r.Draft.Scenes[0].Text = " " }},
		{"excessive text", func(r *Request) { r.Draft.Scenes[0].Text = strings.Repeat("a", 601) }},
		{"unknown quality", func(r *Request) { r.Quality = "ultra" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := exampleRequest()
			tc.change(&req)
			if Validate(&req) == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	for _, raw := range []string{`{"text":"a","visual":"b"}`, `{"speaker":null,"text":"a","visual":"b"}`, `{"speaker":0.5,"text":"a","visual":"b"}`} {
		var scene Scene
		if json.Unmarshal([]byte(raw), &scene) == nil {
			t.Fatalf("accepted invalid speaker in %s", raw)
		}
	}
}

func TestDraftRequestExplicitModes(t *testing.T) {
	for _, req := range []DraftRequest{{Topic: "Made up topic", Provider: "demo"}, {Topic: "Real topic", Provider: "manual"}, {Topic: "hello", Provider: "autopilot"}} {
		if ValidateDraftRequest(&req) == nil {
			t.Fatalf("accepted invalid request: %+v", req)
		}
	}
	req := DraftRequest{Topic: "How binary search works"}
	if err := ValidateDraftRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.Pair != "cog-axiom" || req.TargetSeconds != 60 {
		t.Fatalf("defaults: %+v", req)
	}
}

func waitStatus(t *testing.T, s *Service, id, status string) Job {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		j, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == status {
			return j
		}
		if j.Status == "failed" && status != "failed" {
			t.Fatalf("job failed: %s", j.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	j, _ := s.Get(id)
	t.Fatalf("wanted %s, got %+v", status, j)
	return Job{}
}

func TestRecoveryCancellationAndRetryAreDurable(t *testing.T) {
	release, err := resource.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	dir := t.TempDir()
	db := filepath.Join(dir, "studio.db")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	j := Job{ID: "studio-recovery", Status: "running", Stage: "encoding", Progress: 50, Request: exampleRequest(), CreatedAt: time.Now().UTC()}
	if err := s.save(j); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	recovered, err := s.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "queued" || recovered.Progress != 0 {
		t.Fatalf("not recovered: %+v", recovered)
	}
	if err := s.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.update(j); err != context.Canceled {
		t.Fatalf("late worker update overwrote cancel: %v", err)
	}
	retry, err := s.Retry(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID == j.ID || retry.Status != "queued" {
		t.Fatalf("bad retry: %+v", retry)
	}
	if _, err := s.Retry(retry.ID); err == nil {
		t.Fatal("retried queued job")
	}
	if err := s.Cancel(retry.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("durable list: %+v %v", list, err)
	}
}

func TestRendererProgressAndSafeOutputFiles(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "render.py")
	code := `import argparse,json,pathlib
p=argparse.ArgumentParser();p.add_argument('--input');p.add_argument('--output');a=p.parse_args()
print(json.dumps({'stage':'encoding','progress':65,'message':'Encoding'}),flush=True)
for ext in ['.mp4','.jpg','.srt','.json']:
 pathlib.Path(a.output).with_suffix(ext).write_text('fixture output')
`
	if err := os.WriteFile(script, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUDIO_PYTHON", py)
	t.Setenv("STUDIO_RENDER_SCRIPT", script)
	s, err := New(context.Background(), filepath.Join(dir, "jobs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j, err := s.Create(exampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, s, j.ID, "completed")
	if done.Progress != 100 || !strings.HasSuffix(done.OutputURL, "/files/video.mp4") || len(done.Trace) != 1 {
		t.Fatalf("bad completion: %+v", done)
	}
	file, err := s.OpenOutput(j.ID, "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := s.OpenOutput(j.ID, "request.json"); err == nil {
		t.Fatal("exposed private request")
	}
	if _, err := s.OpenOutput(j.ID, "../../jobs.db"); err == nil {
		t.Fatal("accepted traversal")
	}
	output := filepath.Join(s.root, j.ID, "video.mp4")
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "jobs.db"), output); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenOutput(j.ID, "video.mp4"); err == nil {
		t.Fatal("followed output symlink outside job directory")
	}
}

func TestCancelStopsRunningRenderer(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "slow.py")
	code := `import json,subprocess,time
subprocess.Popen(['sleep','60'])
print(json.dumps({'stage':'speaking','progress':8,'message':'Speaking'}),flush=True)
time.sleep(60)
`
	if err := os.WriteFile(script, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUDIO_PYTHON", py)
	t.Setenv("STUDIO_RENDER_SCRIPT", script)
	s, err := New(context.Background(), filepath.Join(dir, "jobs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Create(exampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, s, j.ID, "running")
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, err := s.Get(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Stage == "speaking" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("renderer did not start its child")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { s.Close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation left renderer children alive")
	}
}

func TestShutdownStopsPlannerAndItsChildren(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required")
	}
	dir := t.TempDir()
	script, marker := filepath.Join(dir, "planner.py"), filepath.Join(dir, "started")
	code := `import os,pathlib,subprocess,time
subprocess.Popen(['sleep','60'])
pathlib.Path(os.environ['STUDIO_TEST_MARKER']).write_text('started')
time.sleep(60)
`
	if err := os.WriteFile(script, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUDIO_PYTHON", py)
	t.Setenv("STUDIO_PLANNER_SCRIPT", script)
	t.Setenv("STUDIO_TEST_MARKER", marker)
	s, err := New(context.Background(), filepath.Join(dir, "jobs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := s.Plan(context.Background(), DraftRequest{Topic: "Why is the sky blue?"})
		result <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			s.Close()
			t.Fatal("planner did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	finished := make(chan struct{})
	go func() { s.Close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown left planner running")
	}
	if err := <-result; err == nil {
		t.Fatal("interrupted planner reported success")
	}
}
