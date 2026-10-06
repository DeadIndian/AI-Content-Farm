package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Gollabharath/ai-content-farm/internal/job"
	"github.com/Gollabharath/ai-content-farm/internal/pipeline"
	"github.com/Gollabharath/ai-content-farm/internal/settings"
	"github.com/Gollabharath/ai-content-farm/internal/storage"
	"github.com/Gollabharath/ai-content-farm/internal/tts"
)

type Server struct {
	store             *storage.JobStore
	settings          *settings.Store
	runner            *pipeline.Runner
	tts               tts.Client
	onSettingsUpdated func(context.Context, settings.Settings, settings.Settings)
	mux               *http.ServeMux
}

func New(
	store *storage.JobStore,
	settingsStore *settings.Store,
	runner *pipeline.Runner,
	ttsClient tts.Client,
	onSettingsUpdated func(context.Context, settings.Settings, settings.Settings),
) *Server {
	s := &Server{
		store:             store,
		settings:          settingsStore,
		runner:            runner,
		tts:               ttsClient,
		onSettingsUpdated: onSettingsUpdated,
		mux:               http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	webRoot, _ := fs.Sub(uiFS, "web")
	s.mux.Handle("GET /app.js", noCache(http.FileServerFS(webRoot)))
	s.mux.Handle("GET /shorts.js", noCache(http.FileServerFS(webRoot)))
	s.mux.Handle("GET /styles.css", noCache(http.FileServerFS(webRoot)))
	s.mux.Handle("GET /", noCache(http.FileServerFS(webRoot)))
	s.mux.HandleFunc("GET /outputs/", s.handleOutputFile)
	s.mux.HandleFunc("GET /inputs/", s.handleInputFile)

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /v1/scripts/generate", s.handleGenerateScript)
	s.mux.HandleFunc("POST /v1/jobs", s.handleCreateJob)
	s.mux.HandleFunc("GET /v1/jobs", s.handleListJobs)
	s.mux.HandleFunc("DELETE /v1/jobs", s.handleClearJobs)
	s.mux.HandleFunc("POST /v1/jobs/", s.handleRerunJob)
	s.mux.HandleFunc("GET /v1/jobs/", s.handleGetJob)

	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
	s.mux.HandleFunc("GET /api/voices", s.handleListVoices)
	s.mux.HandleFunc("POST /api/voices/preview", s.handlePreviewVoice)
	s.mux.HandleFunc("GET /api/videos", s.handleListVideos)
	s.mux.HandleFunc("GET /api/videos/folders", s.handleListVideoFolders)
	s.mux.HandleFunc("POST /api/videos/folders", s.handleCreateVideoFolder)
	s.mux.HandleFunc("POST /api/videos/folders/rename", s.handleRenameVideoFolder)
	s.mux.HandleFunc("POST /api/videos/move", s.handleMoveVideo)
	s.mux.HandleFunc("POST /api/videos/rename", s.handleRenameVideo)
	s.mux.HandleFunc("POST /api/videos/delete", s.handleDeleteVideo)
	s.mux.HandleFunc("GET /api/videos/generated", s.handleListGeneratedVideos)
	s.mux.HandleFunc("POST /api/videos/generated/delete", s.handleDeleteGeneratedVideo)
	s.mux.HandleFunc("POST /api/videos/upload", s.handleUploadVideos)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGenerateScript(w http.ResponseWriter, r *http.Request) {
	var req job.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if strings.TrimSpace(req.Topic) == "" {
		req.Topic = strings.TrimSpace(req.Prompt)
	}

	generated, err := s.runner.GenerateScript(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, generated)
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req job.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if strings.TrimSpace(req.Topic) == "" {
		req.Topic = strings.TrimSpace(req.Prompt)
	}

	if strings.TrimSpace(req.Topic) == "" && strings.TrimSpace(req.ScriptOverride) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing content: provide topic or script_override"})
		return
	}

	j, err := s.runner.CreateJob(req)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, j)
}

func (s *Server) handleListJobs(w http.ResponseWriter, _ *http.Request) {
	jobs := s.store.List()
	for i := range jobs {
		jobs[i].OutputPath = s.publicOutputPath(jobs[i].OutputPath)
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleClearJobs(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing job id"})
		return
	}
	job, ok := s.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	job.OutputPath = s.publicOutputPath(job.OutputPath)
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleRerunJob(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if !strings.HasSuffix(path, "/rerun") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unsupported job action"})
		return
	}

	id := strings.TrimSuffix(path, "/rerun")
	id = strings.TrimSuffix(id, "/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing job id"})
		return
	}

	oldJob, ok := s.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	if oldJob.Status != job.StatusFailed {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only failed jobs can be re-run"})
		return
	}

	newJob, err := s.runner.CreateJob(oldJob.Request)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, newJob)
}

func (s *Server) publicOutputPath(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	name := filepath.Base(raw)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return "/outputs/" + name
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	setNoCacheHeaders(w)
	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	setNoCacheHeaders(w)
	before, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var update settings.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	cfg, err := s.settings.Update(update)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if s.onSettingsUpdated != nil {
		s.onSettingsUpdated(r.Context(), before, cfg)
	}

	_ = os.MkdirAll(cfg.InputVideosDir, 0o755)
	_ = os.MkdirAll(cfg.OutputVideosDir, 0o755)
	writeJSON(w, http.StatusOK, cfg)
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setNoCacheHeaders(w)
		next.ServeHTTP(w, r)
	})
}

func setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func (s *Server) handleListVoices(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))

	type providerVoiceLister interface {
		ListVoicesForProvider(context.Context, string) ([]tts.Voice, error)
	}
	type providerLanguageLister interface {
		ListSupportedLanguagesForProvider(context.Context, string) ([]string, error)
	}

	var (
		voices []tts.Voice
		err    error
	)
	if provider != "" {
		if lister, ok := s.tts.(providerVoiceLister); ok {
			voices, err = lister.ListVoicesForProvider(r.Context(), provider)
		} else {
			voices, err = s.tts.ListVoices(r.Context())
		}
	} else {
		voices, err = s.tts.ListVoices(r.Context())
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	requestedLanguage := strings.TrimSpace(r.URL.Query().Get("language"))
	if requestedLanguage != "" {
		filtered := make([]tts.Voice, 0, len(voices))
		for _, v := range voices {
			if strings.EqualFold(v.LanguageCode, requestedLanguage) {
				filtered = append(filtered, v)
			}
		}
		voices = filtered
	}

	langsMap := map[string]struct{}{}
	for _, v := range voices {
		if strings.TrimSpace(v.LanguageCode) == "" {
			continue
		}
		langsMap[v.LanguageCode] = struct{}{}
	}
	languages := make([]string, 0, len(langsMap))
	for lang := range langsMap {
		languages = append(languages, lang)
	}
	if provider != "" {
		if lister, ok := s.tts.(providerLanguageLister); ok {
			extra, langErr := lister.ListSupportedLanguagesForProvider(r.Context(), provider)
			if langErr == nil {
				for _, lang := range extra {
					trimmed := strings.TrimSpace(lang)
					if trimmed == "" {
						continue
					}
					langsMap[trimmed] = struct{}{}
				}
			}
		}
		languages = languages[:0]
		for lang := range langsMap {
			languages = append(languages, lang)
		}
	}
	sort.Strings(languages)

	writeJSON(w, http.StatusOK, map[string]any{
		"voices":    voices,
		"languages": languages,
	})
}

func (s *Server) handlePreviewVoice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text     string `json:"text"`
		Voice    string `json:"voice"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	audio, err := s.tts.Preview(r.Context(), req.Text, req.Voice, req.Language)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

func (s *Server) handleListVideos(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = os.MkdirAll(cfg.InputVideosDir, 0o755)

	entries, err := collectVideoEntries(cfg.InputVideosDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleListVideoFolders(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = os.MkdirAll(cfg.InputVideosDir, 0o755)

	folders, err := collectVideoFolders(cfg.InputVideosDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (s *Server) handleCreateVideoFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	folder, err := cleanAssetRelPath(req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if folder == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "folder name cannot be empty"})
		return
	}

	fullPath, err := resolveAssetPath(cfg.InputVideosDir, folder)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "created", "folder": filepath.ToSlash(folder)})
}

func (s *Server) handleRenameVideoFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := renameAssetRelPath(s, req.OldName, req.NewName, true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "renamed"})
}

func (s *Server) handleMoveVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourcePath string `json:"source_path"`
		Folder     string `json:"folder"`
		TargetName string `json:"target_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := moveAssetToFolder(s, req.SourcePath, req.Folder, req.TargetName); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}

func (s *Server) handleRenameVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := renameAssetRelPath(s, req.OldName, req.NewName, false); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "renamed"})
}

func (s *Server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := deleteAssetRelPath(s, req.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListGeneratedVideos(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = os.MkdirAll(cfg.OutputVideosDir, 0o755)

	entries, err := os.ReadDir(cfg.OutputVideosDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type generatedVideo struct {
		Name    string    `json:"name"`
		Path    string    `json:"path"`
		URL     string    `json:"url"`
		ModTime time.Time `json:"mod_time"`
	}
	videos := make([]generatedVideo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		switch ext {
		case ".mp4", ".mov", ".mkv", ".webm":
			st, statErr := entry.Info()
			if statErr != nil {
				continue
			}
			videos = append(videos, generatedVideo{
				Name:    entry.Name(),
				Path:    entry.Name(),
				URL:     "/outputs/" + entry.Name(),
				ModTime: st.ModTime().UTC(),
			})
		}
	}

	sort.Slice(videos, func(i, j int) bool {
		if !videos[i].ModTime.Equal(videos[j].ModTime) {
			return videos[i].ModTime.After(videos[j].ModTime)
		}
		return videos[i].Name < videos[j].Name
	})
	writeJSON(w, http.StatusOK, videos)
}

func (s *Server) handleDeleteGeneratedVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	name := filepath.Base(strings.TrimSpace(req.Name))
	if name == "" || name == "." || name == "/" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid video name"})
		return
	}
	fullPath := filepath.Join(cfg.OutputVideosDir, name)
	if err := os.Remove(fullPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleUploadVideos(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(cfg.InputVideosDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart payload"})
		return
	}

	files := r.MultipartForm.File["videos"]
	defer r.MultipartForm.RemoveAll()
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files in form field 'videos'"})
		return
	}

	uploaded := make([]string, 0, len(files))
	for _, header := range files {
		name := filepath.Base(header.Filename)
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".mp4", ".mov", ".mkv", ".webm":
		default:
			continue
		}

		src, err := header.Open()
		if err != nil {
			continue
		}
		dstPath := filepath.Join(cfg.InputVideosDir, name)
		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			continue
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		_ = src.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(dstPath)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save uploaded video"})
			return
		}
		uploaded = append(uploaded, name)
	}

	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}

func (s *Server) handleOutputFile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.settings.Get()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveNamedFile(w, r, "/outputs/", cfg.OutputVideosDir)
}

func (s *Server) handleInputFile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.settings.Get()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveRelativeFile(w, r, "/inputs/", cfg.InputVideosDir)
}

func serveNamedFile(w http.ResponseWriter, r *http.Request, prefix, root string) {
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, prefix))
	if name == "" || name == "." || name == "/" {
		http.NotFound(w, r)
		return
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(root, name))
}

type listedVideo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Folder     string `json:"folder,omitempty"`
	FolderRank int    `json:"folder_rank,omitempty"`
	URL        string `json:"url"`
}

func collectVideoEntries(root string) ([]listedVideo, error) {
	items := make([]listedVideo, 0, 64)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".mp4", ".mov", ".mkv", ".webm":
		default:
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		folder := filepath.ToSlash(filepath.Dir(rel))
		if folder == "." {
			folder = ""
		}

		folderRank := 2
		switch {
		case strings.HasPrefix(folder, "uploads/"):
			folderRank = 0
		case folder != "":
			folderRank = 1
		}

		items = append(items, listedVideo{
			Name:       filepath.Base(rel),
			Path:       rel,
			Folder:     folder,
			FolderRank: folderRank,
			URL:        "/inputs/" + rel,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].FolderRank != items[j].FolderRank {
			return items[i].FolderRank < items[j].FolderRank
		}
		if items[i].Folder != items[j].Folder {
			return items[i].Folder < items[j].Folder
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Path < items[j].Path
	})

	return items, nil
}

func serveRelativeFile(w http.ResponseWriter, r *http.Request, prefix, root string) {
	rel, err := cleanAssetRelPath(strings.TrimPrefix(r.URL.Path, prefix))
	if err != nil || rel == "" {
		http.NotFound(w, r)
		return
	}

	fullPath, err := resolveAssetPath(root, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(fullPath); err != nil {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, fullPath)
}

type listedFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

func collectVideoFolders(root string) ([]listedFolder, error) {
	folders := make([]listedFolder, 0, 32)
	seen := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == root || !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}
		if _, ok := seen[rel]; ok {
			return nil
		}
		seen[rel] = struct{}{}
		folders = append(folders, listedFolder{Name: filepath.Base(rel), Path: rel, URL: "/inputs/" + rel})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(folders, func(i, j int) bool {
		if strings.HasPrefix(folders[i].Path, "uploads/") != strings.HasPrefix(folders[j].Path, "uploads/") {
			return strings.HasPrefix(folders[i].Path, "uploads/")
		}
		return folders[i].Path < folders[j].Path
	})

	return folders, nil
}

func cleanAssetRelPath(value string) (string, error) {
	rel := filepath.ToSlash(strings.TrimSpace(value))
	rel = strings.TrimPrefix(rel, "/")
	rel = filepath.Clean(rel)
	if rel == "" || rel == "." || rel == "/" || rel == string(filepath.Separator) {
		return "", nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path")
	}
	return filepath.ToSlash(rel), nil
}

func resolveAssetPath(root, rel string) (string, error) {
	rel, err := cleanAssetRelPath(rel)
	if err != nil {
		return "", err
	}
	if rel == "" {
		return "", fmt.Errorf("invalid path")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	fullPath := filepath.Join(root, filepath.FromSlash(rel))
	rootClean := filepath.Clean(root)
	fullClean := filepath.Clean(fullPath)
	if fullClean != rootClean && !strings.HasPrefix(fullClean, rootClean+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path")
	}
	return fullPath, nil
}

func videoRootPath(s *Server) (string, error) {
	cfg, err := s.settings.Get()
	if err != nil {
		return "", err
	}
	return cfg.InputVideosDir, nil
}

func renameAssetRelPath(s *Server, oldName, newName string, folderOnly bool) error {
	root, err := videoRootPath(s)
	if err != nil {
		return err
	}
	oldRel, err := cleanAssetRelPath(oldName)
	if err != nil {
		return err
	}
	newRel, err := cleanAssetRelPath(newName)
	if err != nil {
		return err
	}
	if oldRel == "" || newRel == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !strings.Contains(newRel, "/") {
		parent := filepath.ToSlash(filepath.Dir(oldRel))
		if parent != "." && parent != "" {
			newRel = filepath.ToSlash(filepath.Join(parent, newRel))
		}
	}
	oldPath, err := resolveAssetPath(root, oldRel)
	if err != nil {
		return err
	}
	newPath, err := resolveAssetPath(root, newRel)
	if err != nil {
		return err
	}
	if folderOnly {
		if stat, statErr := os.Stat(oldPath); statErr != nil || !stat.IsDir() {
			return fmt.Errorf("folder not found")
		}
	} else {
		if _, statErr := os.Stat(oldPath); statErr != nil {
			return fmt.Errorf("item not found")
		}
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	return nil
}

func moveAssetToFolder(s *Server, sourcePath, folder, targetName string) error {
	root, err := videoRootPath(s)
	if err != nil {
		return err
	}
	sourceRel, err := cleanAssetRelPath(sourcePath)
	if err != nil {
		return err
	}
	if sourceRel == "" {
		return fmt.Errorf("source path cannot be empty")
	}
	sourceFull, err := resolveAssetPath(root, sourceRel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(sourceFull); err != nil {
		return fmt.Errorf("source not found")
	}
	folderRel, err := cleanAssetRelPath(folder)
	if err != nil {
		return err
	}
	targetDir := root
	if folderRel != "" {
		targetDir, err = resolveAssetPath(root, folderRel)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	baseName := filepath.Base(sourceFull)
	if strings.TrimSpace(targetName) != "" {
		baseName = filepath.Base(strings.TrimSpace(targetName))
	}
	targetPath := filepath.Join(targetDir, baseName)
	if err := os.Rename(sourceFull, targetPath); err != nil {
		return err
	}
	return nil
}

func deleteAssetRelPath(s *Server, name string) error {
	root, err := videoRootPath(s)
	if err != nil {
		return err
	}
	rel, err := cleanAssetRelPath(name)
	if err != nil {
		return err
	}
	if rel == "" {
		return fmt.Errorf("path cannot be empty")
	}
	fullPath, err := resolveAssetPath(root, rel)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(fullPath); err != nil {
		return err
	}
	return nil
}

func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
