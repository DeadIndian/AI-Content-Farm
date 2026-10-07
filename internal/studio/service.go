// Package studio manages durable presenter-video jobs and a LangGraph planner.
package studio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Gollabharath/ai-content-farm/internal/resource"
	_ "modernc.org/sqlite"
)

type Service struct {
	db       *sql.DB
	root     string
	castRoot string
	castMu   sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	wake     chan struct{}
	planners chan struct{}
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	wg       sync.WaitGroup
}

func New(ctx context.Context, dbPath, storageDir string) (*Service, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS studio_jobs (id TEXT PRIMARY KEY, payload TEXT NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	root, err := filepath.Abs(filepath.Join(storageDir, "studio"))
	if err == nil {
		err = os.MkdirAll(root, 0755)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	castRoot, err := filepath.Abs(env("STUDIO_CAST_DIR", filepath.Join(storageDir, "cast")))
	if err != nil {
		cancel()
		db.Close()
		return nil, err
	}
	s := &Service{db: db, root: root, castRoot: castRoot, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), planners: make(chan struct{}, 1), active: map[string]context.CancelFunc{}}
	jobs, err := s.List()
	if err != nil {
		cancel()
		db.Close()
		return nil, err
	}
	for _, j := range jobs {
		if j.Status == "running" {
			j.Status, j.Stage, j.Message, j.Progress = "queued", "queued", "Recovered after restart; render will restart", 0
			if err := s.save(j); err != nil {
				cancel()
				db.Close()
				return nil, err
			}
		}
	}
	s.wg.Add(1)
	go s.worker()
	s.notify()
	return s, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	s.db.Close()
}

// Track request workers as well as queued renders so shutdown cannot orphan a
// planner or dependency-check process. Admission and Close share the same lock.
func (s *Service) operation(parent context.Context, timeout time.Duration) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return nil, nil, s.ctx.Err()
	}
	s.wg.Add(1)
	ctx, cancel := context.WithTimeout(parent, timeout)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel(); s.wg.Done() }, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultVoice() string {
	if value := os.Getenv("STUDIO_TTS_PROVIDER"); value != "" {
		return value
	}
	if os.Getenv("GEMINI_API_KEY") != "" {
		return "gemini"
	}
	return "espeak"
}

func python() string {
	for _, key := range []string{"STUDIO_PYTHON", "SHORTS_PYTHON", "PYTHON_BIN"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	if _, err := os.Stat(".venv/bin/python3"); err == nil {
		return ".venv/bin/python3"
	}
	return "python3"
}

// Cancellation must include speech/FFmpeg children, not just the Python parent.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func (s *Service) workerCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := command(ctx, python(), args...)
	cmd.Env = append(os.Environ(), "STUDIO_CAST_DIR="+s.castRoot)
	return cmd
}

type tailBuffer struct{ data []byte }

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.data = append(b.data, p...)
	if len(b.data) > 12000 {
		b.data = b.data[len(b.data)-12000:]
	}
	return n, nil
}

func (s *Service) Plan(ctx context.Context, req DraftRequest) (Draft, error) {
	if err := ValidateDraftRequest(&req); err != nil {
		return Draft{}, err
	}
	ctx, finish, err := s.operation(ctx, 75*time.Second)
	if err != nil {
		return Draft{}, err
	}
	defer finish()
	select {
	case s.planners <- struct{}{}:
		defer func() { <-s.planners }()
	case <-ctx.Done():
		return Draft{}, ctx.Err()
	}
	cmd := s.workerCommand(ctx, env("STUDIO_PLANNER_SCRIPT", "scripts/studio_plan.py"), "--checkpoints", filepath.Join(s.root, "planning.sqlite3"))
	raw, _ := json.Marshal(req)
	cmd.Stdin = bytes.NewReader(raw)
	var stderr tailBuffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return Draft{}, fmt.Errorf("planning timed out or was canceled: %w", ctx.Err())
		}
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(stderr.data), &failure) == nil && failure.Error != "" {
			return Draft{}, errors.New(failure.Error)
		}
		return Draft{}, fmt.Errorf("planner failed: %w; %s", err, strings.TrimSpace(string(stderr.data)))
	}
	var draft Draft
	if err := json.Unmarshal(out, &draft); err != nil {
		return Draft{}, fmt.Errorf("planner returned invalid JSON: %w", err)
	}
	validated := Request{Draft: draft, Quality: "preview"}
	if err := Validate(&validated); err != nil {
		return Draft{}, fmt.Errorf("planner returned an invalid draft: %w", err)
	}
	return validated.Draft, nil
}

func (s *Service) probe(ctx context.Context, script string) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := s.workerCommand(ctx, script, "--capabilities")
	var stderr tailBuffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	var result map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	if err != nil {
		return map[string]any{"ready": false, "reason": "Worker dependency check failed. Install requirements-studio.txt and check STUDIO_PYTHON.", "detail": err.Error()}
	}
	return result
}

func (s *Service) Capabilities(ctx context.Context) map[string]any {
	ctx, finish, err := s.operation(ctx, 10*time.Second)
	if err != nil {
		unavailable := map[string]any{"ready": false, "reason": "Studio is shutting down"}
		return map[string]any{"planner": unavailable, "renderer": unavailable, "pairs": []any{}, "demo_topics": []string{}, "limitations": []string{}}
	}
	defer finish()
	var planner, renderer map[string]any
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		planner = s.probe(ctx, env("STUDIO_PLANNER_SCRIPT", "scripts/studio_plan.py"))
	}()
	go func() {
		defer wg.Done()
		renderer = s.probe(ctx, env("STUDIO_RENDER_SCRIPT", "scripts/studio_render.py"))
	}()
	wg.Wait()
	pairs, ok := renderer["pairs"]
	if !ok {
		pairs = []any{}
	}
	if entries, ok := pairs.([]any); ok {
		for _, raw := range entries {
			pair, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := pair["id"].(string)
			characters, _ := pair["characters"].([]any)
			urls := []string{}
			for _, rawCharacter := range characters {
				character, ok := rawCharacter.(map[string]any)
				if !ok {
					continue
				}
				filename, _ := character["idle"].(string)
				if pair["builtin"] == false {
					urls = append(urls, "/api/studio/cast/"+id+"/"+filename)
				} else if id == "cog-axiom" || id == "nova-atlas" {
					urls = append(urls, "/api/studio/presenters/"+filename)
				}
			}
			if len(urls) == 2 {
				pair["sprite_urls"] = urls
			}
		}
	}
	topics, ok := planner["demo_topics"]
	if !ok {
		topics = []string{"Why is the sky blue?", "How binary search works", "What happens inside a black hole?"}
	}
	return map[string]any{"planner": planner, "renderer": renderer, "pairs": pairs, "demo_topics": topics,
		"shorts":  map[string]any{"provider": env("SHORTS_TRANSCRIBER", "gemini"), "cloud_ready": os.Getenv("GEMINI_API_KEY") != "", "ready": env("SHORTS_TRANSCRIBER", "gemini") == "gemini" && os.Getenv("GEMINI_API_KEY") != "", "local_models_allowed": strings.EqualFold(os.Getenv("ALLOW_LOCAL_MODELS"), "true")},
		"runtime": map[string]any{"local_models_enabled": strings.EqualFold(os.Getenv("ALLOW_LOCAL_MODELS"), "true"), "render_profile": env("STUDIO_RESOURCE_MODE", "gentle"), "default_voice_provider": defaultVoice()},
		"limitations": []string{
			"Review scripts and factual claims before rendering or publishing. Planning does not perform web research.",
			"Demo mode uses curated scripts. Any-topic AI mode requires a configured model provider.",
			"Offline speech is synthetic; preview exports prioritize fast review. Full exports use higher resolution.",
			"Personal presenter pairs require local assets you have permission to use.",
		}}
}

func (s *Service) save(j Job) error {
	j.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO studio_jobs(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, j.ID, string(raw))
	return err
}

func (s *Service) List() ([]Job, error) {
	rows, err := s.db.Query(`SELECT payload FROM studio_jobs ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var j Job
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Service) Get(id string) (Job, error) {
	var j Job
	var raw string
	err := s.db.QueryRow(`SELECT payload FROM studio_jobs WHERE id=?`, id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &j)
	}
	return j, err
}

func (s *Service) Create(req Request) (Job, error) {
	if err := Validate(&req); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Job{}, fmt.Errorf("studio is shutting down")
	}
	jobs, err := s.List()
	if err != nil {
		return Job{}, err
	}
	pending := 0
	for _, j := range jobs {
		if j.Status == "queued" || j.Status == "running" {
			pending++
		}
	}
	if pending >= 20 {
		return Job{}, fmt.Errorf("render queue is full; wait for an existing job to finish")
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Job{}, err
	}
	j := Job{ID: "studio-" + hex.EncodeToString(id[:]), Status: "queued", Stage: "queued", Message: "Waiting for the render worker", CreatedAt: time.Now().UTC(), Request: req, Trace: append([]Trace{}, req.Draft.Trace...)}
	j.UpdatedAt = j.CreatedAt
	if err := s.save(j); err != nil {
		return Job{}, err
	}
	s.notify()
	return j, nil
}

func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.Get(id)
	if err != nil {
		return err
	}
	if j.Status != "queued" && j.Status != "running" {
		return fmt.Errorf("job is already %s", j.Status)
	}
	j.Status, j.Stage, j.Message = "canceled", "canceled", "Render canceled"
	if err := s.save(j); err != nil {
		return err
	}
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	return nil
}

func (s *Service) Retry(id string) (Job, error) {
	j, err := s.Get(id)
	if err != nil {
		return Job{}, err
	}
	if j.Status != "failed" && j.Status != "canceled" {
		return Job{}, fmt.Errorf("only failed or canceled jobs can be retried")
	}
	return s.Create(j.Request)
}

func (s *Service) update(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.Get(j.ID)
	if err != nil {
		return err
	}
	if old.Status == "canceled" {
		return context.Canceled
	}
	return s.save(j)
}

func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) worker() {
	defer s.wg.Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		jobs, err := s.List()
		if err != nil {
			log.Printf("studio queue read: %v", err)
			continue
		}
		for i := len(jobs) - 1; i >= 0; i-- {
			if s.ctx.Err() != nil {
				return
			}
			if jobs[i].Status == "queued" {
				s.process(jobs[i].ID)
			}
		}
	}
}

func (s *Service) process(id string) {
	s.mu.Lock()
	j, err := s.Get(id)
	if err != nil || j.Status != "queued" {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	s.active[id] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }()
	release, err := resource.Acquire(ctx)
	if err != nil {
		return
	}
	defer release()
	j.Status, j.Stage, j.Message = "running", "starting", "Starting presenter render"
	if err := s.update(j); err != nil {
		return
	}
	started := time.Now()
	err = s.execute(ctx, &j)
	if err != nil {
		j.Status, j.Stage, j.Error, j.Message = "failed", "failed", err.Error(), "Render failed; fix the cause and retry"
		if s.ctx.Err() != nil {
			j.Status, j.Stage, j.Error, j.Message, j.Progress = "queued", "queued", "", "Paused; will resume after restart", 0
		}
	} else {
		j.Status, j.Stage, j.Progress, j.Message = "completed", "completed", 100, "Video ready to preview and download"
		base := "/api/studio/jobs/" + j.ID + "/files/"
		j.OutputURL, j.PosterURL, j.CaptionURL = base+"video.mp4", base+"poster.jpg", base+"captions.srt"
	}
	j.Trace = append(j.Trace, Trace{Node: "render", Status: j.Status, Detail: j.Message, DurationMS: time.Since(started).Milliseconds()})
	if err := s.update(j); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("studio save result %s: %v", id, err)
	}
}

func (s *Service) execute(ctx context.Context, j *Job) error {
	work := filepath.Join(s.root, j.ID)
	if err := os.MkdirAll(work, 0755); err != nil {
		return err
	}
	// Python's finally block cannot run after SIGKILL. Remove only its known
	// scratch prefix after command completion/cancellation, retaining final media.
	defer func() {
		matches, _ := filepath.Glob(filepath.Join(work, ".studio-render-*"))
		for _, path := range matches {
			_ = os.RemoveAll(path)
		}
	}()
	raw, err := json.Marshal(j.Request)
	if err != nil {
		return err
	}
	input, output := filepath.Join(work, "request.json"), filepath.Join(work, "video.mp4")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		return err
	}
	cmd := s.workerCommand(ctx, env("STUDIO_RENDER_SCRIPT", "scripts/studio_render.py"), "--input", input, "--output", output)
	var stderr tailBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 1024*1024)
	var updateErr error
	for scanner.Scan() {
		var event struct {
			Stage    string `json:"stage"`
			Progress int    `json:"progress"`
			Message  string `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Stage != "" {
			j.Stage = event.Stage
		}
		if event.Message != "" {
			j.Message = event.Message
		}
		if event.Progress > j.Progress && event.Progress < 100 {
			j.Progress = event.Progress
		}
		if err := s.update(*j); err != nil {
			updateErr = err
			_ = cmd.Cancel()
			break
		}
	}
	if scanner.Err() != nil {
		_ = cmd.Cancel()
	}
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	if updateErr != nil {
		return updateErr
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	if ctx.Err() != nil {
		return fmt.Errorf("render interrupted: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("presenter renderer failed: %w; %s", err, strings.TrimSpace(string(stderr.data)))
	}
	for _, ext := range []string{".mp4", ".jpg", ".srt", ".json"} {
		st, err := os.Stat(filepath.Join(work, "video"+ext))
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return fmt.Errorf("renderer did not produce a valid %s output", ext)
		}
	}
	return nil
}

// OpenOutput only exposes fixed output names for completed jobs. os.Root prevents
// an output symlink from escaping its job directory, including on file replacement.
func (s *Service) OpenOutput(id, name string) (*os.File, error) {
	j, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if j.Status != "completed" {
		return nil, os.ErrNotExist
	}
	names := map[string]string{"video.mp4": "video.mp4", "poster.jpg": "video.jpg", "captions.srt": "video.srt", "manifest.json": "video.json"}
	filename, ok := names[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(filepath.Join(s.root, j.ID))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(filename)
	if err != nil {
		return nil, err
	}
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}
