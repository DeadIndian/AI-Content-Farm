package httpserver

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Gollabharath/ai-content-farm/internal/shorts"
)

func (s *Server) RegisterShorts(service *shorts.Service) {
	s.mux.HandleFunc("POST /api/videos/import-youtube", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/api/shorts"
		s.mux.ServeHTTP(w, r)
	})
	s.mux.HandleFunc("POST /api/shorts", func(w http.ResponseWriter, r *http.Request) {
		var req shorts.Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON payload"})
			return
		}
		if err := shorts.Validate(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		cfg, err := s.settings.Get()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		j, err := service.Create(req, cfg.InputVideosDir, cfg.OutputVideosDir)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, j)
	})
	s.mux.HandleFunc("GET /api/shorts", func(w http.ResponseWriter, r *http.Request) {
		setNoCacheHeaders(w)
		jobs, err := service.List()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, jobs)
	})
	s.mux.HandleFunc("GET /api/shorts/{id}", func(w http.ResponseWriter, r *http.Request) {
		setNoCacheHeaders(w)
		j, err := service.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "job not found"})
			return
		}
		writeJSON(w, 200, j)
	})
	s.mux.HandleFunc("POST /api/shorts/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := service.Cancel(r.PathValue("id")); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "cancelled"})
	})
	s.mux.HandleFunc("GET /api/shorts/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		j, err := service.Get(r.PathValue("id"))
		if err != nil || len(j.Clips) == 0 {
			http.NotFound(w, r)
			return
		}
		// Check before sending ZIP headers; a deleted file should be a clear error.
		for _, clip := range j.Clips {
			if _, err := os.Stat(filepath.Join(j.OutputDir, clip.Filename)); err != nil {
				http.Error(w, "A clip was deleted from disk", 404)
				return
			}
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+j.ID+`.zip"`)
		archive := zip.NewWriter(w)
		defer archive.Close()
		manifest, err := archive.Create("manifest.json")
		if err != nil {
			return
		}
		if err := json.NewEncoder(manifest).Encode(j); err != nil {
			return
		}
		for _, clip := range j.Clips {
			if r.Context().Err() != nil {
				return
			}
			file, err := os.Open(filepath.Join(j.OutputDir, clip.Filename))
			if err != nil {
				return
			}
			entry, err := archive.CreateHeader(&zip.FileHeader{Name: clip.Filename, Method: zip.Store})
			if err != nil {
				file.Close()
				return
			}
			_, err = io.Copy(entry, file)
			file.Close()
			if err != nil {
				return
			}
		}
	})
}
