package studio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Scene struct {
	Speaker int    `json:"speaker"`
	Text    string `json:"text"`
	Visual  string `json:"visual"`
}

func (s *Scene) UnmarshalJSON(raw []byte) error {
	var value struct {
		Speaker *int   `json:"speaker"`
		Text    string `json:"text"`
		Visual  string `json:"visual"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if value.Speaker == nil {
		return fmt.Errorf("scene speaker must be the integer 0 or 1")
	}
	s.Speaker, s.Text, s.Visual = *value.Speaker, value.Text, value.Visual
	return nil
}

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Trace struct {
	Node       string `json:"node"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	DurationMS int64  `json:"duration_ms"`
}

type DraftRequest struct {
	Topic         string `json:"topic"`
	SourceNotes   string `json:"source_notes"`
	Pair          string `json:"pair"`
	Format        string `json:"format"`
	TargetSeconds int    `json:"target_seconds"`
	Provider      string `json:"provider"`
}

type Draft struct {
	Title            string   `json:"title"`
	Topic            string   `json:"topic"`
	Pair             string   `json:"pair"`
	Format           string   `json:"format"`
	Provider         string   `json:"provider"`
	Scenes           []Scene  `json:"scenes"`
	Sources          []Source `json:"sources"`
	Warnings         []string `json:"warnings"`
	Trace            []Trace  `json:"trace"`
	EstimatedSeconds int      `json:"estimated_seconds"`
}

type Request struct {
	Draft         Draft  `json:"draft"`
	Quality       string `json:"quality"`
	VoiceProvider string `json:"voice_provider,omitempty"`
}

type Job struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Stage      string    `json:"stage"`
	Progress   int       `json:"progress"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Request    Request   `json:"request"`
	OutputURL  string    `json:"output_url"`
	PosterURL  string    `json:"poster_url"`
	CaptionURL string    `json:"caption_url"`
	Trace      []Trace   `json:"trace"`
	Error      string    `json:"error,omitempty"`
}

var castID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func ValidCastID(id string) bool { return len(id) <= 64 && castID.MatchString(id) }

func validateOptions(pair, format, provider string) error {
	if !ValidCastID(pair) {
		return fmt.Errorf("presenter pair ID must contain lowercase letters, digits and single hyphens")
	}
	switch format {
	case "portrait", "landscape", "square":
	default:
		return fmt.Errorf("format must be portrait, landscape or square")
	}
	switch provider {
	case "demo", "manual", "ai":
	default:
		return fmt.Errorf("provider must be demo, manual or ai")
	}
	return nil
}

func ValidateDraftRequest(req *DraftRequest) error {
	req.Topic, req.SourceNotes = strings.TrimSpace(req.Topic), strings.TrimSpace(req.SourceNotes)
	if req.Pair == "" {
		req.Pair = "cog-axiom"
	}
	if req.Format == "" {
		req.Format = "portrait"
	}
	if req.Provider == "" {
		req.Provider = "demo"
	}
	if req.TargetSeconds == 0 {
		req.TargetSeconds = 60
	}
	if err := validateOptions(req.Pair, req.Format, req.Provider); err != nil {
		return err
	}
	if len([]rune(req.Topic)) == 0 || len([]rune(req.Topic)) > 240 {
		return fmt.Errorf("topic must contain 1 to 240 characters")
	}
	if len([]rune(req.SourceNotes)) > 18000 {
		return fmt.Errorf("source_notes exceeds 18,000 characters")
	}
	if req.TargetSeconds < 20 || req.TargetSeconds > 180 {
		return fmt.Errorf("target_seconds must be between 20 and 180")
	}
	if req.Provider == "manual" && req.SourceNotes == "" {
		return fmt.Errorf("manual mode requires a script in source_notes")
	}
	if req.Provider == "demo" {
		topic := strings.ToLower(req.Topic)
		if !(strings.Contains(topic, "sky") && strings.Contains(topic, "blue")) && !strings.Contains(topic, "binary search") && !strings.Contains(topic, "black hole") {
			return fmt.Errorf("demo mode supports sky blue, binary search and black holes; choose a demo topic or use manual or AI mode")
		}
	}
	return nil
}

func Validate(req *Request) error {
	if req.VoiceProvider == "" {
		req.VoiceProvider = defaultVoice()
	}
	switch req.VoiceProvider {
	case "espeak", "gemini":
	case "piper":
		if !strings.EqualFold(os.Getenv("ALLOW_LOCAL_MODELS"), "true") {
			return fmt.Errorf("local models are disabled; choose Gemini cloud speech or lightweight eSpeak")
		}
	default:
		return fmt.Errorf("voice_provider must be gemini, espeak or piper")
	}
	if req.Quality == "" {
		req.Quality = "preview"
	}
	if req.Quality != "preview" && req.Quality != "full" {
		return fmt.Errorf("quality must be preview or full")
	}
	d := &req.Draft
	if err := validateOptions(d.Pair, d.Format, d.Provider); err != nil {
		return err
	}
	d.Title, d.Topic = strings.TrimSpace(d.Title), strings.TrimSpace(d.Topic)
	if len([]rune(d.Title)) == 0 || len([]rune(d.Title)) > 160 {
		return fmt.Errorf("title must contain 1 to 160 characters")
	}
	if len([]rune(d.Topic)) == 0 || len([]rune(d.Topic)) > 240 {
		return fmt.Errorf("topic must contain 1 to 240 characters")
	}
	if len(d.Scenes) < 2 || len(d.Scenes) > 24 {
		return fmt.Errorf("a draft needs 2 to 24 scenes")
	}
	var speakers [2]bool
	characters, words := 0, 0
	for i := range d.Scenes {
		scene := &d.Scenes[i]
		if scene.Speaker < 0 || scene.Speaker > 1 {
			return fmt.Errorf("scene %d needs speaker 0 or 1", i+1)
		}
		speakers[scene.Speaker] = true
		scene.Text, scene.Visual = strings.TrimSpace(scene.Text), strings.TrimSpace(scene.Visual)
		n := len([]rune(scene.Text))
		if n < 1 || n > 600 {
			return fmt.Errorf("scene %d dialogue must contain 1 to 600 characters", i+1)
		}
		if len([]rune(scene.Visual)) < 1 || len([]rune(scene.Visual)) > 140 {
			return fmt.Errorf("scene %d visual must contain 1 to 140 characters", i+1)
		}
		characters += n
		words += len(strings.Fields(scene.Text))
	}
	if !speakers[0] || !speakers[1] {
		return fmt.Errorf("both presenters must have dialogue")
	}
	if characters > 6000 {
		return fmt.Errorf("dialogue exceeds 6,000 characters")
	}
	if len(d.Sources) > 20 || len(d.Warnings) > 30 || len(d.Trace) > 100 {
		return fmt.Errorf("draft metadata exceeds the supported size")
	}
	for _, source := range d.Sources {
		u, err := url.Parse(source.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(source.URL) > 2048 || len(source.Title) > 300 {
			return fmt.Errorf("source URLs must be valid HTTP(S) URLs")
		}
	}
	if d.Sources == nil {
		d.Sources = []Source{}
	}
	if d.Warnings == nil {
		d.Warnings = []string{}
	}
	if d.Trace == nil {
		d.Trace = []Trace{}
	}
	// Recalculate after edits instead of trusting a stale client estimate.
	d.EstimatedSeconds = int(float64(words)/2.35 + float64(len(d.Scenes))*0.3 + 0.5)
	return nil
}
