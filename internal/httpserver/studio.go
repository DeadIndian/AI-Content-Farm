package httpserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Gollabharath/ai-content-farm/internal/studio"
)

func studioDecode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func studioError(w http.ResponseWriter, status int, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) RegisterStudio(service *studio.Service) {
	s.mux.HandleFunc("GET /api/studio/capabilities", func(w http.ResponseWriter, r *http.Request) {
		setNoCacheHeaders(w)
		writeJSON(w, http.StatusOK, service.Capabilities(r.Context()))
	})
	s.mux.HandleFunc("POST /api/studio/drafts", func(w http.ResponseWriter, r *http.Request) {
		var req studio.DraftRequest
		if err := studioDecode(w, r, &req); err != nil {
			studioError(w, 400, err)
			return
		}
		if err := studio.ValidateDraftRequest(&req); err != nil {
			studioError(w, 422, err)
			return
		}
		caps := service.Capabilities(r.Context())
		if !studio.AvailablePair(caps, req.Pair) {
			studioError(w, 422, errors.New("Presenter pair is unavailable; choose an installed cast or upload its sprites"))
			return
		}
		planner, _ := caps["planner"].(map[string]any)
		if planner["ready"] != true {
			studioError(w, 503, errors.New("LangGraph planner is unavailable; install requirements-studio.txt and check STUDIO_PYTHON"))
			return
		}
		if req.Provider == "ai" && planner["ai_configured"] != true {
			studioError(w, 503, errors.New("AI mode requires GEMINI_API_KEY, or LLM_API_KEY and LLM_MODEL (OpenRouter is also supported)"))
			return
		}
		draft, err := service.Plan(r.Context(), req)
		if err != nil {
			studioError(w, 502, err)
			return
		}
		writeJSON(w, 200, draft)
	})
	s.mux.HandleFunc("POST /api/studio/jobs", func(w http.ResponseWriter, r *http.Request) {
		var req studio.Request
		if err := studioDecode(w, r, &req); err != nil {
			studioError(w, 400, err)
			return
		}
		if err := studio.Validate(&req); err != nil {
			studioError(w, 422, err)
			return
		}
		caps := service.Capabilities(r.Context())
		renderer, _ := caps["renderer"].(map[string]any)
		if renderer["ffmpeg"] != true || renderer["pillow"] != true {
			studioError(w, 503, errors.New("Presenter renderer is unavailable; check Studio settings for missing dependencies"))
			return
		}
		if !studio.AvailablePair(caps, req.Draft.Pair) {
			studioError(w, 422, errors.New("Presenter pair is unavailable; choose an installed cast or upload its sprites"))
			return
		}
		if !studio.AvailableVoice(caps, req.VoiceProvider) {
			studioError(w, 503, errors.New("Selected speech provider is unavailable; choose an available provider in Studio"))
			return
		}
		job, err := service.Create(req)
		if err != nil {
			studioError(w, 400, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	})
	s.mux.HandleFunc("GET /api/studio/jobs", func(w http.ResponseWriter, r *http.Request) {
		setNoCacheHeaders(w)
		jobs, err := service.List()
		if err != nil {
			studioError(w, 500, err)
			return
		}
		writeJSON(w, 200, jobs)
	})
	s.mux.HandleFunc("GET /api/studio/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		setNoCacheHeaders(w)
		job, err := service.Get(r.PathValue("id"))
		if err != nil {
			studioError(w, 500, err)
			return
		}
		writeJSON(w, 200, job)
	})
	s.mux.HandleFunc("POST /api/studio/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := service.Cancel(r.PathValue("id")); err != nil {
			studioError(w, 409, err)
			return
		}
		job, err := service.Get(r.PathValue("id"))
		if err != nil {
			studioError(w, 500, err)
			return
		}
		writeJSON(w, 200, job)
	})
	s.mux.HandleFunc("POST /api/studio/jobs/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		job, err := service.Retry(r.PathValue("id"))
		if err != nil {
			studioError(w, 409, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	})
	s.mux.HandleFunc("GET /api/studio/jobs/{id}/files/{name}", func(w http.ResponseWriter, r *http.Request) {
		file, err := service.OpenOutput(r.PathValue("id"), r.PathValue("name"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.PathValue("name") == "captions.srt" {
			w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
		}
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("name")+`"`)
		}
		http.ServeContent(w, r, r.PathValue("name"), stat.ModTime(), file)
	})
	s.mux.HandleFunc("GET /api/studio/presenters/{filename}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("filename")
		allowed := false
		for _, person := range []string{"nova", "atlas", "cog", "axiom"} {
			for _, pose := range []string{"idle", "talk", "blink"} {
				if name == person+"-"+pose+".png" {
					allowed = true
				}
			}
		}
		if !allowed || strings.ContainsAny(name, "/\\") {
			http.NotFound(w, r)
			return
		}
		root, err := os.OpenRoot("assets/presenters")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		file, err := root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		http.ServeContent(w, r, name, stat.ModTime(), file)
	})
	s.mux.HandleFunc("POST /api/studio/cast", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
		if err := r.ParseMultipartForm(2 * 1024 * 1024); err != nil {
			studioError(w, 400, errors.New("Upload must be multipart data under 16 MiB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		fields := map[string]bool{"id": true, "name": true, "speaker_a": true, "speaker_b": true, "role_a": true, "role_b": true}
		for key, values := range r.MultipartForm.Value {
			if !fields[key] || len(values) != 1 {
				studioError(w, 400, errors.New("Unknown or repeated cast field"))
				return
			}
		}
		allowedFiles := map[string]bool{"idle_a": true, "idle_b": true, "talk_a": true, "talk_b": true, "blink_a": true, "blink_b": true}
		readers := map[string]io.Reader{}
		for key, files := range r.MultipartForm.File {
			if !allowedFiles[key] || len(files) != 1 {
				studioError(w, 400, errors.New("Unknown or repeated sprite field"))
				return
			}
			if files[0].Size > 4*1024*1024 {
				studioError(w, 400, errors.New("Each PNG must be under 4 MiB"))
				return
			}
			file, err := files[0].Open()
			if err != nil {
				studioError(w, 400, err)
				return
			}
			defer file.Close()
			readers[key] = file
		}
		pair, err := service.InstallCast(r.Context(), studio.CastUpload{ID: r.FormValue("id"), Name: r.FormValue("name"), SpeakerA: r.FormValue("speaker_a"), SpeakerB: r.FormValue("speaker_b"), RoleA: r.FormValue("role_a"), RoleB: r.FormValue("role_b"), Files: readers})
		if err != nil {
			studioError(w, 422, err)
			return
		}
		writeJSON(w, http.StatusCreated, pair)
	})
	s.mux.HandleFunc("GET /api/studio/cast/{id}/{filename}", func(w http.ResponseWriter, r *http.Request) {
		file, err := service.OpenCastSprite(r.PathValue("id"), r.PathValue("filename"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		st, err := file.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, r.PathValue("filename"), st.ModTime(), file)
	})
}
