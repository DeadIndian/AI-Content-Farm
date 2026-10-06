package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                 string
	StorageDir           string
	DatabasePath         string
	InputVideosDir       string
	OutputVideosDir      string
	GeminiAPIKey         string
	OpenRouterAPIKey     string
	OpenRouterModel      string
	TTSProvider          string
	TTSDockerAutoManage  bool
	TTSDockerServiceName string
	TTSDockerProjectDir  string
	TTSBaseURL           string
	TTSSynthPath         string
	TTSTimeoutSecs       int
	ElevenLabsAPIKey     string
	ElevenLabsBaseURL    string
	ElevenLabsModelID    string
	ElevenLabsVoiceID    string
	ElevenLabsOutputFmt  string
	FFmpegBinaryPath     string
	AutoPilotEnabled     bool
	AutoPilotEvery       int
	AutoPrompt           string
	AutoVoice            string
}

func Load() (Config, error) {
	cfg := Config{
		Port:                 envOrDefault("PORT", "8080"),
		StorageDir:           envOrDefault("STORAGE_DIR", "./data"),
		DatabasePath:         envOrDefault("DB_PATH", "app.db"),
		InputVideosDir:       envOrDefault("INPUT_VIDEOS_DIR", "./videos"),
		OutputVideosDir:      envOrDefault("OUTPUT_VIDEOS_DIR", "./data/generated"),
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		OpenRouterAPIKey:     os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:      envOrDefault("OPENROUTER_MODEL", "google/gemini-2.0-flash-001"),
		TTSProvider:          envOrDefault("TTS_PROVIDER", "piper"),
		TTSDockerAutoManage:  envBoolOrDefault("TTS_DOCKER_AUTO_MANAGE", true),
		TTSDockerServiceName: envOrDefault("TTS_DOCKER_SERVICE_NAME", "aicf-tts"),
		TTSDockerProjectDir:  envOrDefault("TTS_DOCKER_PROJECT_DIR", ""),
		TTSBaseURL:           envOrDefault("TTS_BASE_URL", "http://localhost:5002"),
		TTSSynthPath:         envOrDefault("TTS_SYNTH_PATH", "/api/tts"),
		TTSTimeoutSecs:       envIntOrDefault("TTS_TIMEOUT_SECONDS", 30),
		ElevenLabsAPIKey:     os.Getenv("ELEVENLABS_API_KEY"),
		ElevenLabsBaseURL:    envOrDefault("ELEVENLABS_BASE_URL", "https://api.elevenlabs.io"),
		ElevenLabsModelID:    envOrDefault("ELEVENLABS_MODEL_ID", "eleven_multilingual_v2"),
		ElevenLabsVoiceID:    os.Getenv("ELEVENLABS_VOICE_ID"),
		ElevenLabsOutputFmt:  envOrDefault("ELEVENLABS_OUTPUT_FORMAT", "mp3_44100_128"),
		FFmpegBinaryPath:     envOrDefault("FFMPEG_BIN", "ffmpeg"),
		AutoPilotEnabled:     envBoolOrDefault("AUTOPILOT_ENABLED", false),
		AutoPilotEvery:       envIntOrDefault("AUTOPILOT_EVERY_SECONDS", 1800),
		AutoPrompt:           envOrDefault("AUTOPILOT_PROMPT", "Create one punchy, high-retention short script with a strong hook."),
		AutoVoice:            os.Getenv("AUTOPILOT_VOICE"),
	}

	if _, err := strconv.Atoi(cfg.Port); err != nil {
		return Config{}, fmt.Errorf("invalid PORT %q: %w", cfg.Port, err)
	}
	if cfg.TTSTimeoutSecs < 1 {
		return Config{}, fmt.Errorf("TTS_TIMEOUT_SECONDS must be >= 1")
	}
	if strings.TrimSpace(cfg.TTSDockerServiceName) == "" {
		return Config{}, fmt.Errorf("TTS_DOCKER_SERVICE_NAME cannot be empty")
	}
	cfg.TTSProvider = strings.ToLower(strings.TrimSpace(cfg.TTSProvider))
	switch cfg.TTSProvider {
	case "", "piper", "elevenlabs", "auto":
		if cfg.TTSProvider == "" {
			cfg.TTSProvider = "piper"
		}
	default:
		return Config{}, fmt.Errorf("invalid TTS_PROVIDER %q: expected piper, elevenlabs, or auto", cfg.TTSProvider)
	}
	if strings.TrimSpace(cfg.InputVideosDir) == "" || strings.TrimSpace(cfg.OutputVideosDir) == "" {
		return Config{}, fmt.Errorf("INPUT_VIDEOS_DIR and OUTPUT_VIDEOS_DIR are required")
	}
	if cfg.AutoPilotEvery < 30 {
		return Config{}, fmt.Errorf("AUTOPILOT_EVERY_SECONDS must be >= 30")
	}

	return cfg, nil
}

func envIntOrDefault(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

func envOrDefault(key, defaultValue string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	return v
}

func envBoolOrDefault(key string, defaultValue bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultValue
	}
	return b
}
