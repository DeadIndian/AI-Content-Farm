package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gollabharath/ai-content-farm/internal/studio"
)

// Exercise the real PNG validator and atomic publication without encoding media.
func TestStudioCastUploadValidationAndServing(t *testing.T) {
	project, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(project, ".venv/bin/python3")
	if _, err := os.Stat(python); err != nil {
		python = "python3"
	}
	if err := exec.Command(python, "-c", "from PIL import Image").Run(); err != nil {
		t.Skip("Pillow is required for the cast upload integration check")
	}
	t.Setenv("STUDIO_PYTHON", python)
	t.Setenv("STUDIO_CAST_SCRIPT", filepath.Join(project, "scripts/studio_cast.py"))
	dir := t.TempDir()
	service, err := studio.New(context.Background(), filepath.Join(dir, "studio.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := &Server{mux: http.NewServeMux()}
	server.RegisterStudio(service)
	var sprite bytes.Buffer
	if err := png.Encode(&sprite, image.NewRGBA(image.Rect(0, 0, 32, 40))); err != nil {
		t.Fatal(err)
	}
	upload := func(id string, data []byte, status int) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for key, value := range map[string]string{"id": id, "name": "Test Cast", "speaker_a": "First", "speaker_b": "Second"} {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		for _, key := range []string{"idle_a", "idle_b"} {
			part, err := writer.CreateFormFile(key, "untrusted-original.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/api/studio/cast", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("upload %s: status %d, wanted %d: %s", id, response.Code, status, response.Body)
		}
	}
	upload("test-pair", sprite.Bytes(), 201)
	upload("test-pair", sprite.Bytes(), 422)
	upload("cog-axiom", sprite.Bytes(), 422)
	upload("../escape", sprite.Bytes(), 422)
	upload("broken-pair", sprite.Bytes()[:40], 422)
	for path, status := range map[string]int{
		"/api/studio/cast/test-pair/idle_a.png":   200,
		"/api/studio/cast/test-pair/idle_b.png":   200,
		"/api/studio/cast/test-pair/cast.json":    404,
		"/api/studio/cast/test-pair/missing.png":  404,
		"/api/studio/cast/broken-pair/idle_a.png": 404,
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != status {
			t.Fatalf("%s: %d, wanted %d", path, response.Code, status)
		}
		if status == 200 && !bytes.Equal(response.Body.Bytes(), sprite.Bytes()) {
			t.Fatal("sprite download changed the uploaded bytes")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "cast"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "test-pair" {
		t.Fatalf("unvalidated or temporary files remained: %v %v", entries, err)
	}
}

func TestStudioHTTPRejectsInvalidRequestsAndMissingResources(t *testing.T) {
	dir := t.TempDir()
	service, err := studio.New(context.Background(), filepath.Join(dir, "studio.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := &Server{mux: http.NewServeMux()}
	server.RegisterStudio(service)
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/studio/drafts", `{"topic":"x","extra":"unknown"}`, 400},
		{"POST", "/api/studio/drafts", `{"topic":"x"} {"topic":"y"}`, 400},
		{"POST", "/api/studio/drafts", `{"topic":"x","provider":"manual"}`, 422},
		{"POST", "/api/studio/drafts", `{"topic":"invented topic","provider":"demo"}`, 422},
		{"POST", "/api/studio/jobs", `{"quality":"preview","draft":{}}`, 422},
		{"POST", "/api/studio/jobs", `{"draft":{"scenes":[{"text":"hello","visual":"hello"}]}}`, 400},
		{"GET", "/api/studio/jobs/missing", "", 404},
		{"POST", "/api/studio/jobs/missing/cancel", "", 404},
		{"POST", "/api/studio/jobs/missing/retry", "", 404},
		{"GET", "/api/studio/jobs/missing/files/request.json", "", 404},
		{"GET", "/api/studio/presenters/secret.env", "", 404},
		{"GET", "/api/studio/jobs", "", 200},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status %d, wanted %d: %s", recorder.Code, tc.status, recorder.Body)
			}
		})
	}
}

// Opt-in end-to-end test uses actual LangGraph, local speech and FFmpeg, then
// downloads the result through the HTTP handler. It needs no listening socket.
func TestStudioRealPipeline(t *testing.T) {
	project := os.Getenv("STUDIO_INTEGRATION_ROOT")
	if project == "" {
		t.Skip("set STUDIO_INTEGRATION_ROOT to run real planning and rendering")
	}
	python := os.Getenv("STUDIO_PYTHON")
	if python == "" {
		python = filepath.Join(project, ".venv/bin/python3")
		if _, err := os.Stat(python); err != nil {
			python = "python3"
		}
	}
	t.Setenv("STUDIO_PYTHON", python)
	t.Setenv("STUDIO_TTS_PROVIDER", "espeak")
	t.Setenv("STUDIO_PLANNER_SCRIPT", filepath.Join(project, "scripts/studio_plan.py"))
	t.Setenv("STUDIO_RENDER_SCRIPT", filepath.Join(project, "scripts/studio_render.py"))
	dir := t.TempDir()
	service, err := studio.New(context.Background(), filepath.Join(dir, "studio.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := &Server{mux: http.NewServeMux()}
	server.RegisterStudio(service)
	call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s %s: status %d, wanted %d: %s", method, path, response.Code, status, response.Body)
		}
		return response
	}
	response := call("POST", "/api/studio/drafts", studio.DraftRequest{Topic: "Binary search in brief", Provider: "manual", Pair: "nova-atlas", Format: "portrait", TargetSeconds: 20,
		SourceNotes: "Binary search starts in the middle of a sorted list. Each comparison removes half the remaining candidates."}, 200)
	var draft studio.Draft
	if err := json.Unmarshal(response.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if len(draft.Trace) != 5 || len(draft.Scenes) != 2 {
		t.Fatalf("invalid graph draft: %+v", draft)
	}
	response = call("POST", "/api/studio/jobs", studio.Request{Draft: draft, Quality: "preview"}, 202)
	var job studio.Job
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		response = call("GET", "/api/studio/jobs/"+job.ID, nil, 200)
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == "completed" {
			break
		}
		if job.Status == "failed" {
			t.Fatalf("real renderer failed: %s", job.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("render timed out: %+v", job)
		}
		time.Sleep(250 * time.Millisecond)
	}
	video := call("GET", job.OutputURL, nil, 200).Body.Bytes()
	if len(video) < 1000 || !bytes.Equal(video[4:8], []byte("ftyp")) {
		t.Fatal("download was not an MP4")
	}
	call("GET", job.PosterURL, nil, 200)
	captions := call("GET", job.CaptionURL, nil, 200).Body.String()
	if !strings.Contains(captions, "-->") || !strings.Contains(captions, "Binary") {
		t.Fatalf("invalid captions: %s", captions)
	}
	path := filepath.Join(dir, "download.mp4")
	if err := os.WriteFile(path, video, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "json", path).Output()
	if err != nil || !bytes.Contains(result, []byte(`"audio"`)) || !bytes.Contains(result, []byte(`"video"`)) {
		t.Fatalf("invalid media streams: %s %v", result, err)
	}
	t.Logf("LangGraph to downloadable MP4: %d bytes, %d scenes, %d trace events", len(video), len(draft.Scenes), len(job.Trace))
}
