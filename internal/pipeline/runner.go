package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gollabharath/ai-content-farm/internal/job"
	"github.com/Gollabharath/ai-content-farm/internal/resource"
	"github.com/Gollabharath/ai-content-farm/internal/script"
	"github.com/Gollabharath/ai-content-farm/internal/settings"
	"github.com/Gollabharath/ai-content-farm/internal/storage"
	"github.com/Gollabharath/ai-content-farm/internal/tts"
	"github.com/Gollabharath/ai-content-farm/internal/video"
)

type Runner struct {
	jobs     chan string
	store    *storage.JobStore
	settings *settings.Store
	script   script.Generator
	tts      tts.Client
	video    video.Builder
	workers  int
	wg       sync.WaitGroup
}

var idCounter uint64

func NewRunner(store *storage.JobStore, settingsStore *settings.Store, scriptGenerator script.Generator, ttsClient tts.Client, videoBuilder video.Builder, workers int) *Runner {
	if workers < 1 {
		workers = 1
	}
	return &Runner{
		jobs:     make(chan string, 10000),
		store:    store,
		settings: settingsStore,
		script:   scriptGenerator,
		tts:      ttsClient,
		video:    videoBuilder,
		workers:  workers,
	}
}

func (r *Runner) Start(ctx context.Context) {
	r.requeuePersistedJobs()

	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go func(workerID int) {
			defer r.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id, ok := <-r.jobs:
					if !ok {
						return
					}
					r.process(ctx, workerID, id)
				}
			}
		}(i + 1)
	}
}

func (r *Runner) requeuePersistedJobs() {
	jobs := r.store.List()
	for _, j := range jobs {
		if j.Status != job.StatusQueued && j.Status != job.StatusRunning {
			continue
		}
		select {
		case r.jobs <- j.ID:
		default:
			return
		}
	}
}

func (r *Runner) Stop() {
	close(r.jobs)
	r.wg.Wait()
}

func (r *Runner) Enqueue(jobID string) error {
	select {
	case r.jobs <- jobID:
		return nil
	default:
		return fmt.Errorf("job queue is full")
	}
}

func (r *Runner) CreateJob(req job.Request) (job.Job, error) {
	id := "job-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(atomic.AddUint64(&idCounter, 1), 10)
	now := time.Now().UTC()
	j := job.Job{
		ID:        id,
		Status:    job.StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
		Request:   req,
	}

	r.store.Save(j)
	if err := r.Enqueue(id); err != nil {
		return job.Job{}, err
	}
	return j, nil
}

func (r *Runner) GenerateScript(ctx context.Context, req job.Request) (script.GeneratedContent, error) {
	return r.script.Generate(ctx, req)
}

func (r *Runner) process(ctx context.Context, workerID int, jobID string) {
	release, err := resource.Acquire(ctx)
	if err != nil {
		return
	}
	defer release()
	j, ok := r.store.Get(jobID)
	if !ok {
		return
	}

	j.Status = job.StatusRunning
	j.UpdatedAt = time.Now().UTC()
	r.store.Save(j)

	cfg, err := r.settings.Get()
	if err != nil {
		r.failJob(j, err)
		return
	}

	if err := os.MkdirAll(cfg.OutputVideosDir, 0o755); err != nil {
		r.failJob(j, err)
		return
	}

	scriptText := strings.TrimSpace(j.Request.ScriptOverride)
	scriptsByLang := map[string]string{}
	if scriptText == "" {
		generated, genErr := r.script.Generate(ctx, j.Request)
		err = genErr
		if err != nil {
			r.failJob(j, err)
			return
		}
		scriptText = strings.TrimSpace(generated.Script)
		for k, v := range generated.Scripts {
			key := strings.ToLower(strings.TrimSpace(k))
			value := strings.TrimSpace(v)
			if key == "" || value == "" {
				continue
			}
			scriptsByLang[key] = value
		}
	} else {
		scriptsByLang["english"] = scriptText
	}
	for k, v := range j.Request.Scripts {
		key := strings.ToLower(strings.TrimSpace(k))
		value := strings.TrimSpace(v)
		if key == "" || value == "" {
			continue
		}
		scriptsByLang[key] = value
	}
	if scriptText == "" {
		scriptText = strings.TrimSpace(scriptsByLang["english"])
	}
	if scriptText == "" {
		scriptText = resolveFirstScript(scriptsByLang)
	}
	if scriptText == "" {
		r.failJob(j, fmt.Errorf("generated script was empty"))
		return
	}
	j.Script = scriptText
	j.UpdatedAt = time.Now().UTC()
	r.store.Save(j)

	voice := strings.TrimSpace(j.Request.Voice)
	if voice == "" {
		voice = strings.TrimSpace(cfg.DefaultVoice)
	}
	orientation := strings.TrimSpace(j.Request.Orientation)
	if orientation == "" {
		orientation = cfg.DefaultVideoOrientation
	}
	customWidth := j.Request.CustomWidth
	customHeight := j.Request.CustomHeight
	if customWidth <= 0 {
		customWidth = cfg.DefaultVideoWidth
	}
	if customHeight <= 0 {
		customHeight = cfg.DefaultVideoHeight
	}

	topic := strings.TrimSpace(j.Request.Topic)
	if topic == "" {
		topic = strings.TrimSpace(j.Request.Prompt)
	}

	variants := []struct {
		name     string
		langCode string
	}{
		{name: "english", langCode: "en"},
		{name: "hindi", langCode: "hi"},
		{name: "telugu", langCode: "te"},
	}

	var firstVideoPath string
	for _, variant := range variants {
		variantScript := pickScriptForVariant(scriptsByLang, variant.name, scriptText)
		voiceoverPath, synthErr := r.synthesizeWithRetry(ctx, variantScript, voice, variant.langCode, cfg.OutputVideosDir)
		if synthErr != nil {
			r.failJob(j, synthErr)
			return
		}

		slug := sanitize(fmt.Sprintf("%s-%s", topic, variant.name))
		videoPath, renderErr := r.video.Render(ctx, video.RenderRequest{
			AudioPath:       voiceoverPath,
			Topic:           slug,
			OutputDir:       cfg.OutputVideosDir,
			InputVideosDir:  cfg.InputVideosDir,
			BackgroundVideo: j.Request.BackgroundVideo,
			Orientation:     orientation,
			CustomWidth:     customWidth,
			CustomHeight:    customHeight,
		})
		if renderErr != nil {
			r.failJob(j, renderErr)
			return
		}

		if firstVideoPath == "" {
			firstVideoPath = videoPath
		}
	}

	j.Status = job.StatusCompleted
	j.OutputPath = firstVideoPath
	j.UpdatedAt = time.Now().UTC()
	r.store.Save(j)
}

func pickScriptForVariant(scripts map[string]string, variant string, fallback string) string {
	key := strings.ToLower(strings.TrimSpace(variant))
	if key != "" {
		if value := strings.TrimSpace(scripts[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(scripts["english"]); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func resolveFirstScript(scripts map[string]string) string {
	for _, key := range []string{"english", "hindi", "telugu"} {
		if value := strings.TrimSpace(scripts[key]); value != "" {
			return value
		}
	}
	for _, value := range scripts {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (r *Runner) synthesizeWithRetry(ctx context.Context, text, voice, language, outputDir string) (string, error) {
	voiceoverPath, err := r.tts.Synthesize(ctx, text, voice, language, outputDir)
	if err == nil {
		return voiceoverPath, nil
	}

	for attempt := 1; attempt <= 2; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}

		voiceoverPath, err = r.tts.Synthesize(ctx, text, voice, language, outputDir)
		if err == nil {
			return voiceoverPath, nil
		}
	}

	return "", err
}

func (r *Runner) failJob(j job.Job, err error) {
	j.Status = job.StatusFailed
	j.ErrorMessage = err.Error()
	j.UpdatedAt = time.Now().UTC()
	r.store.Save(j)
}

func sanitize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "untitled"
	}
	const maxSlugLen = 80
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
		if s == "" {
			return "untitled"
		}
	}
	return filepath.Clean(s)
}
