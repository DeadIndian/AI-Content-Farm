package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gollabharath/ai-content-farm/internal/shorts"
)

func TestShortsRegenerationHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Exercise the real durable API without launching media workers.
	dir := t.TempDir()
	service, err := shorts.New(ctx, filepath.Join(dir, "jobs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	original, err := service.Create(shorts.Request{URL: "https://youtu.be/example"}, dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(original.ID); err != nil {
		t.Fatal(err)
	}
	server := &Server{mux: http.NewServeMux()}
	server.RegisterShorts(service)
	for _, tc := range []struct {
		id, body string
		status   int
	}{
		{original.ID, `{"max_duration":40,"edit_style":"reel","cut_mode":"fixed","clip_start":0}`, 202},
		{original.ID, `{"max_duration":46}`, 400},
		{original.ID, `{"clip_start":-1}`, 400},
		{original.ID, `{"edit_style":"arbitrary-filter"}`, 400},
		{original.ID, `broken`, 400},
		{"missing", `{}`, 404},
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/api/shorts/"+tc.id+"/regenerate", strings.NewReader(tc.body)))
		if response.Code != tc.status {
			t.Fatalf("%s: %d: %s", tc.body, response.Code, response.Body)
		}
		if response.Code == 202 {
			var job shorts.Job
			if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
			if job.ID == original.ID || job.Request.URL != original.Request.URL || job.Request.ClipStart == nil || *job.Request.ClipStart != 0 {
				t.Fatalf("bad regenerated job: %+v", job)
			}
		}
	}
}
