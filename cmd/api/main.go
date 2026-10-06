package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Gollabharath/ai-content-farm/internal/autopilot"
	"github.com/Gollabharath/ai-content-farm/internal/config"
	"github.com/Gollabharath/ai-content-farm/internal/httpserver"
	"github.com/Gollabharath/ai-content-farm/internal/pipeline"
	"github.com/Gollabharath/ai-content-farm/internal/script"
	"github.com/Gollabharath/ai-content-farm/internal/settings"
	"github.com/Gollabharath/ai-content-farm/internal/shorts"
	"github.com/Gollabharath/ai-content-farm/internal/storage"
	"github.com/Gollabharath/ai-content-farm/internal/tts"
	"github.com/Gollabharath/ai-content-farm/internal/video"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.StorageDir, 0o755); err != nil {
		log.Fatalf("storage dir init error: %v", err)
	}

	dbPath := cfg.DatabasePath
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(cfg.StorageDir, dbPath)
	}

	settingsStore, err := settings.NewStore(dbPath, settings.Settings{
		InputVideosDir:          cfg.InputVideosDir,
		OutputVideosDir:         cfg.OutputVideosDir,
		DefaultVideoOrientation: "portrait",
		DefaultVideoWidth:       1080,
		DefaultVideoHeight:      1920,
		TTSProvider:             cfg.TTSProvider,
		PiperEnabled:            cfg.TTSProvider == tts.ProviderPiper,
		DefaultVoice:            "",
		DefaultLanguage:         "",
		DefaultPromptIdea:       "",
		PromptPresets:           []settings.Preset{},
	})
	if err != nil {
		log.Fatalf("settings init error: %v", err)
	}

	store, err := storage.NewJobStoreWithFile(dbPath)
	if err != nil {
		log.Fatalf("storage init error: %v", err)
	}
	var composeManager *tts.ComposeManager
	if cfg.TTSDockerAutoManage {
		composeManager = tts.NewComposeManager(
			tts.ResolveProjectDir(cfg.TTSDockerProjectDir),
			cfg.TTSDockerServiceName,
		)
	}

	syncTTSDocker := func(_ context.Context, piperEnabled bool) {
		if composeManager == nil {
			return
		}
		syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := composeManager.SyncPiperEnabled(syncCtx, piperEnabled); err != nil {
			log.Printf("tts docker sync failed for piper_enabled=%t: %v", piperEnabled, err)
		}
	}

	settingsFromStore := func() settings.Settings {
		current, err := settingsStore.Get()
		if err != nil {
			return settings.Settings{TTSProvider: cfg.TTSProvider, PiperEnabled: cfg.TTSProvider == tts.ProviderPiper}
		}
		return current
	}

	currentSettings := settingsFromStore()
	syncTTSDocker(ctx, currentSettings.PiperEnabled)

	piperClient := tts.NewHTTPClient(cfg.TTSBaseURL, cfg.TTSSynthPath, time.Duration(cfg.TTSTimeoutSecs)*time.Second)
	elevenLabsClient := tts.NewElevenLabsClient(tts.ElevenLabsConfig{
		APIKey:       cfg.ElevenLabsAPIKey,
		BaseURL:      cfg.ElevenLabsBaseURL,
		DefaultVoice: cfg.ElevenLabsVoiceID,
		DefaultModel: cfg.ElevenLabsModelID,
		OutputFormat: cfg.ElevenLabsOutputFmt,
		Timeout:      time.Duration(cfg.TTSTimeoutSecs) * time.Second,
	})
	ttsClient := tts.NewProviderRouter(
		piperClient,
		elevenLabsClient,
		func(context.Context) string {
			return settingsFromStore().TTSProvider
		},
		func(ctx context.Context) {
			provider := tts.ProviderPiper
			piperEnabled := true
			if _, err := settingsStore.Update(settings.Update{TTSProvider: &provider, PiperEnabled: &piperEnabled}); err != nil {
				log.Printf("auto fallback to piper failed to persist: %v", err)
				return
			}
			syncTTSDocker(ctx, piperEnabled)
		},
	)
	runner := pipeline.NewRunner(
		store,
		settingsStore,
		script.NewGeminiOpenRouterGenerator(cfg.GeminiAPIKey, cfg.OpenRouterAPIKey, cfg.OpenRouterModel, 60*time.Second),
		ttsClient,
		video.NewFFmpegBuilder(cfg.FFmpegBinaryPath),
		1,
	)
	runner.Start(ctx)
	defer runner.Stop()

	if cfg.AutoPilotEnabled {
		go autopilot.Start(ctx, runner, autopilot.Config{
			EverySeconds: cfg.AutoPilotEvery,
			Prompt:       cfg.AutoPrompt,
			Voice:        cfg.AutoVoice,
		})
		log.Printf("autopilot enabled: every %ds", cfg.AutoPilotEvery)
	}

	srv := httpserver.New(store, settingsStore, runner, ttsClient, func(ctx context.Context, before, after settings.Settings) {
		if strings.EqualFold(before.TTSProvider, after.TTSProvider) && before.PiperEnabled == after.PiperEnabled {
			return
		}
		syncTTSDocker(ctx, after.PiperEnabled)
	})
	shortsService, err := shorts.New(ctx, dbPath, cfg.StorageDir)
	if err != nil {
		log.Fatalf("podcast service init error: %v", err)
	}
	defer shortsService.Close()
	defer stop()
	srv.RegisterShorts(shortsService)
	addr := ":" + cfg.Port
	log.Printf("api listening on %s", addr)

	if err := httpserver.ListenAndServe(ctx, addr, srv.Handler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
