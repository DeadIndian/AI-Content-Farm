package shorts

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
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

type Request struct {
	URL         string   `json:"url"`
	Source      string   `json:"source,omitempty"`
	Layout      string   `json:"layout"`
	Language    string   `json:"language,omitempty"`
	MaxDuration float64  `json:"max_duration"`
	NoCaptions  bool     `json:"no_captions"`
	CutMode     string   `json:"cut_mode,omitempty"`
	EditStyle   string   `json:"edit_style,omitempty"`
	ClipStart   *float64 `json:"clip_start,omitempty"`
}

type Clip struct {
	Filename string  `json:"filename"`
	URL      string  `json:"url"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Duration float64 `json:"duration"`
	Title    string  `json:"title"`
	Text     string  `json:"text"`
}

type Job struct {
	ID             string    `json:"id"`
	Request        Request   `json:"request"`
	Status         string    `json:"status"`
	Stage          string    `json:"stage"`
	Progress       int       `json:"progress"`
	Message        string    `json:"message"`
	Error          string    `json:"error,omitempty"`
	Warnings       []string  `json:"warnings,omitempty"`
	Clips          []Clip    `json:"clips"`
	Total          int       `json:"total"`
	Completed      int       `json:"completed"`
	SourceDuration float64   `json:"source_duration"`
	SourceJobID    string    `json:"source_job_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	InputDir       string    `json:"-"`
	OutputDir      string    `json:"-"`
}

type Service struct {
	db     *sql.DB
	root   string
	ctx    context.Context
	wake   chan struct{}
	mu     sync.Mutex
	active map[string]context.CancelFunc
	wg     sync.WaitGroup
}

func New(ctx context.Context, dbPath, storageDir string) (*Service, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS shorts_jobs (
		id TEXT PRIMARY KEY, payload TEXT NOT NULL, input_dir TEXT NOT NULL, output_dir TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	root, err := filepath.Abs(filepath.Join(storageDir, "shorts"))
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Service{db: db, root: root, ctx: ctx, wake: make(chan struct{}, 1), active: map[string]context.CancelFunc{}}
	jobs, err := s.List()
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, j := range jobs {
		// Jobs created by a native run store absolute host paths. Rebase only
		// unavailable paths when moving the same data directory into Docker.
		if os.Getenv("AICF_CONTAINER") == "true" {
			if _, err := os.Stat(j.InputDir); os.IsNotExist(err) {
				j.InputDir, _ = filepath.Abs(env("INPUT_VIDEOS_DIR", "./videos"))
			}
			if _, err := os.Stat(j.OutputDir); os.IsNotExist(err) {
				j.OutputDir, _ = filepath.Abs(env("OUTPUT_VIDEOS_DIR", "./data/generated"))
			}
			if _, err := db.Exec(`UPDATE shorts_jobs SET input_dir=?, output_dir=? WHERE id=?`, j.InputDir, j.OutputDir, j.ID); err != nil {
				db.Close()
				return nil, err
			}
		}
		if j.Status == "running" {
			j.Status, j.Message = "queued", "Resuming after restart"
			if err := s.save(j); err != nil {
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

func (s *Service) Close() { s.wg.Wait(); s.db.Close() }

func Validate(req *Request) error {
	req.URL, req.Source = strings.TrimSpace(req.URL), strings.TrimSpace(req.Source)
	if (req.URL == "") == (req.Source == "") {
		return fmt.Errorf("provide either a YouTube URL or a library source")
	}
	if req.URL != "" {
		if !strings.Contains(req.URL, "://") {
			req.URL = "https://" + req.URL
		}
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
			return fmt.Errorf("invalid YouTube URL")
		}
		switch strings.ToLower(u.Hostname()) {
		case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		default:
			return fmt.Errorf("provide a youtube.com or youtu.be video URL")
		}
		if u.Path == "" || u.Path == "/" || u.Path == "/playlist" {
			return fmt.Errorf("provide a single video URL")
		}
	}
	if req.MaxDuration == 0 {
		req.MaxDuration = 45
	}
	if math.IsNaN(req.MaxDuration) || req.MaxDuration < 5 || req.MaxDuration > 45 {
		return fmt.Errorf("max_duration must be between 5 and 45 seconds")
	}
	if req.Layout == "" {
		req.Layout = "fit"
	}
	switch req.Layout {
	case "fit", "crop", "split":
	default:
		return fmt.Errorf("layout must be fit, crop or split")
	}
	if req.CutMode == "" {
		req.CutMode = "sentence"
	}
	if req.CutMode != "sentence" && req.CutMode != "fixed" {
		return fmt.Errorf("cut_mode must be sentence or fixed")
	}
	if req.EditStyle == "" {
		req.EditStyle = "clean"
	}
	if req.EditStyle != "clean" && req.EditStyle != "reel" {
		return fmt.Errorf("edit_style must be clean or reel")
	}
	if req.ClipStart != nil && (math.IsNaN(*req.ClipStart) || math.IsInf(*req.ClipStart, 0) || *req.ClipStart < 0) {
		return fmt.Errorf("clip_start must be a finite, nonnegative source time")
	}
	return nil
}

func (s *Service) Create(req Request, inputDir, outputDir string) (Job, error) {
	return s.create(req, inputDir, outputDir, "")
}

// Regenerate creates an independent export while sharing only source/transcript
// caches. Clients cannot select arbitrary cache paths or replace the source.
func (s *Service) Regenerate(id string, req Request) (Job, error) {
	parent, err := s.Get(id)
	if err != nil {
		return Job{}, err
	}
	if parent.Status != "completed" && parent.Status != "failed" && parent.Status != "cancelled" {
		return Job{}, fmt.Errorf("wait for the original job to finish or cancel it before editing")
	}
	req.URL, req.Source = parent.Request.URL, parent.Request.Source
	if req.ClipStart != nil && parent.SourceDuration > 0 && *req.ClipStart >= parent.SourceDuration {
		return Job{}, fmt.Errorf("clip_start must be before the end of the source")
	}
	cacheID := parent.SourceJobID
	if cacheID == "" {
		cacheID = parent.ID
	}
	return s.create(req, parent.InputDir, parent.OutputDir, cacheID)
}

func (s *Service) create(req Request, inputDir, outputDir, cacheID string) (Job, error) {
	if err := Validate(&req); err != nil {
		return Job{}, err
	}
	inputDir, err := filepath.Abs(inputDir)
	if err != nil {
		return Job{}, err
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return Job{}, err
	}
	if req.Source != "" {
		if _, err := localSource(inputDir, req.Source); err != nil {
			return Job{}, err
		}
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Job{}, err
	}
	j := Job{ID: "shorts-" + hex.EncodeToString(id[:]), Request: req, Status: "queued", Stage: "queued",
		Message: "Waiting for podcast worker", Clips: []Clip{}, CreatedAt: time.Now().UTC(), InputDir: inputDir, OutputDir: outputDir, SourceJobID: cacheID}
	j.UpdatedAt = j.CreatedAt
	if err := s.save(j); err != nil {
		return Job{}, err
	}
	s.notify()
	return j, nil
}

func localSource(root, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("source must be relative to the video library")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("source video not found: %w", err)
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source must be inside video library")
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() {
		return "", fmt.Errorf("source is not a file")
	}
	return resolved, nil
}

func (s *Service) save(j Job) error {
	j.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO shorts_jobs VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, j.ID, string(raw), j.InputDir, j.OutputDir)
	return err
}

func (s *Service) List() ([]Job, error) {
	rows, err := s.db.Query(`SELECT payload, input_dir, output_dir FROM shorts_jobs ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var raw, in, out string
		if err := rows.Scan(&raw, &in, &out); err != nil {
			return nil, err
		}
		var j Job
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			return nil, err
		}
		j.InputDir, j.OutputDir = in, out
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Service) Get(id string) (Job, error) {
	var raw string
	var j Job
	err := s.db.QueryRow(`SELECT payload, input_dir, output_dir FROM shorts_jobs WHERE id=?`, id).Scan(&raw, &j.InputDir, &j.OutputDir)
	if err != nil {
		return j, err
	}
	err = json.Unmarshal([]byte(raw), &j)
	return j, err
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
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	j.Status, j.Stage, j.Message = "cancelled", "cancelled", "Cancelled; completed clips are retained"
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
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-time.After(3 * time.Second):
		}
		jobs, err := s.List()
		if err != nil {
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

func (s *Service) update(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.Get(j.ID)
	if err != nil {
		return err
	}
	if old.Status == "cancelled" {
		return context.Canceled
	}
	return s.save(j)
}

func (s *Service) process(id string) {
	release, err := resource.Acquire(s.ctx)
	if err != nil {
		return
	}
	defer release()
	s.mu.Lock()
	j, err := s.Get(id)
	if err != nil || j.Status != "queued" {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.active[id] = cancel
	j.Status = "running"
	err = s.save(j)
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }()
	if err != nil {
		return
	}
	err = s.execute(ctx, &j)
	if err != nil {
		j.Status, j.Stage, j.Error = "failed", "failed", err.Error()
		if s.ctx.Err() != nil {
			j.Status, j.Stage, j.Error, j.Message = "queued", "queued", "", "Paused; will resume on restart"
		}
	} else {
		j.Status, j.Stage, j.Progress = "completed", "completed", 100
	}
	_ = s.update(j)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// Kill the whole process group, including ffmpeg spawned by Python/yt-dlp.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
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

func (s *Service) execute(ctx context.Context, j *Job) error {
	// Apply additive defaults when resuming jobs saved by older versions.
	if err := Validate(&j.Request); err != nil {
		return err
	}
	work := filepath.Join(s.root, j.ID)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	cache := work
	if j.SourceJobID != "" {
		cache = filepath.Join(s.root, j.SourceJobID)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	source := ""
	if j.Request.Source != "" {
		var err error
		source, err = localSource(j.InputDir, j.Request.Source)
		if err != nil {
			return err
		}
	} else {
		j.Stage, j.Message, j.Progress = "downloading", "Downloading YouTube video (up to 1080p)", 2
		if err := s.update(*j); err != nil {
			return err
		}
		marker := filepath.Join(cache, "download.json")
		if raw, err := os.ReadFile(marker); err == nil {
			_ = json.Unmarshal(raw, &source)
		}
		if _, err := os.Stat(source); source == "" || err != nil {
			ffmpeg, err := exec.LookPath(env("FFMPEG_BIN", "ffmpeg"))
			if err != nil {
				return fmt.Errorf("FFmpeg is required for YouTube downloads: %w", err)
			}
			ffmpeg, err = filepath.Abs(ffmpeg)
			if err != nil {
				return err
			}
			args := []string{"--no-playlist", "--no-simulate", "--no-progress", "--newline", "--socket-timeout", "30", "--retries", "3",
				"--js-runtimes", "deno", "--js-runtimes", "node", "--remote-components", "ejs:github", "--ffmpeg-location", ffmpeg,
				"--concurrent-fragments", "1", "--limit-rate", env("YOUTUBE_DOWNLOAD_RATE", "5M"),
				"-f", "bv*[height<=1080][fps<=30]+ba/b[height<=1080][fps<=30]/bv*[height<=1080]+ba/b[height<=1080]", "--merge-output-format", "mp4",
				"--print", "after_move:filepath", "-o", filepath.Join(cache, "source.%(ext)s")}
			if cookies := os.Getenv("YOUTUBE_COOKIES_FILE"); cookies != "" {
				args = append(args, "--cookies", cookies)
			}
			if browser := os.Getenv("YOUTUBE_COOKIES_BROWSER"); browser != "" {
				args = append(args, "--cookies-from-browser", browser)
			}
			args = append(args, "--", j.Request.URL)
			cmd := command(ctx, env("YTDLP_BIN", "yt-dlp"), args...)
			var stderr tailBuffer
			cmd.Stderr = &stderr
			raw, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("YouTube download failed: %w\n%s\nIf YouTube requires sign-in, configure YOUTUBE_COOKIES_BROWSER or upload the video to the library.", err, stderr.data)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			source = strings.TrimSpace(lines[len(lines)-1])
			if _, err := os.Stat(source); err != nil {
				return fmt.Errorf("download produced no video: %w", err)
			}
			encoded, _ := json.Marshal(source)
			if err := os.WriteFile(marker, encoded, 0o644); err != nil {
				return err
			}
		}
	}
	script, err := filepath.Abs(env("SHORTS_SCRIPT", "scripts/shorts.py"))
	if err != nil {
		return err
	}
	args := []string{script, "--source", source, "--work", work, "--output", j.OutputDir,
		"--prefix", j.ID, "--layout", j.Request.Layout, "--max-duration", fmt.Sprint(j.Request.MaxDuration), "--language", j.Request.Language,
		"--cache", cache, "--cut-mode", j.Request.CutMode, "--edit-style", j.Request.EditStyle}
	if j.Request.ClipStart != nil {
		args = append(args, "--clip-start", fmt.Sprint(*j.Request.ClipStart))
	}
	if j.Request.NoCaptions {
		args = append(args, "--no-captions")
	}
	cmd := command(ctx, env("PYTHON_BIN", "python3"), args...)
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
	scanner.Buffer(make([]byte, 65536), 4*1024*1024)
	var updateErr error
	for scanner.Scan() {
		var event struct {
			Stage          string  `json:"stage"`
			Progress       int     `json:"progress"`
			Message        string  `json:"message"`
			Warning        string  `json:"warning"`
			Total          int     `json:"total"`
			Completed      int     `json:"completed"`
			SourceDuration float64 `json:"source_duration"`
			Clip           *Clip   `json:"clip"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Stage != "" {
			j.Stage, j.Progress, j.Message = event.Stage, event.Progress, event.Message
		}
		if event.Total > 0 {
			j.Total = event.Total
		}
		if event.Completed > 0 {
			j.Completed = event.Completed
		}
		if event.SourceDuration > 0 {
			j.SourceDuration = event.SourceDuration
		}
		if event.Warning != "" {
			j.Warnings = append(j.Warnings, event.Warning)
		}
		if event.Clip != nil {
			found := false
			for _, c := range j.Clips {
				if c.Filename == event.Clip.Filename {
					found = true
					break
				}
			}
			if !found {
				j.Clips = append(j.Clips, *event.Clip)
			}
		}
		if err := s.update(*j); err != nil {
			updateErr = err
			_ = cmd.Cancel()
			break
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	if updateErr != nil {
		return updateErr
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	if err != nil {
		return fmt.Errorf("podcast processing failed: %w\n%s", err, stderr.data)
	}
	if len(j.Clips) == 0 {
		return fmt.Errorf("processor returned no clips")
	}
	// Retain originals for editing unless the operator explicitly favors disk space.
	// Never remove library uploads.
	if env("SHORTS_KEEP_SOURCE", "true") != "true" && j.Request.URL != "" {
		matches, _ := filepath.Glob(filepath.Join(cache, "source.*"))
		for _, path := range matches {
			_ = os.Remove(path)
		}
		_ = os.Remove(filepath.Join(cache, "download.json"))
	}
	return nil
}
