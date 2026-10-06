package video

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Builder interface {
	Render(ctx context.Context, req RenderRequest) (string, error)
}

type RenderRequest struct {
	AudioPath       string
	Topic           string
	OutputDir       string
	InputVideosDir  string
	BackgroundVideo string
	Orientation     string
	CustomWidth     int
	CustomHeight    int
}

type FFmpegBuilder struct {
	bin string
}

func NewFFmpegBuilder(bin string) *FFmpegBuilder {
	if strings.TrimSpace(bin) == "" {
		bin = "ffmpeg"
	}
	return &FFmpegBuilder{bin: bin}
}

func (b *FFmpegBuilder) Render(ctx context.Context, req RenderRequest) (string, error) {
	if req.AudioPath == "" {
		return "", fmt.Errorf("audio path cannot be empty")
	}
	if _, err := os.Stat(req.AudioPath); err != nil {
		return "", fmt.Errorf("audio file unavailable: %w", err)
	}

	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	topic := req.Topic
	if topic == "" {
		topic = "untitled"
	}
	slug := sanitize(topic)
	outPath := filepath.Join(req.OutputDir, fmt.Sprintf("%s-%d.mp4", slug, time.Now().UnixNano()))

	width, height := resolveSize(req.Orientation, req.CustomWidth, req.CustomHeight)
	bgPath, err := resolveBackground(req.InputVideosDir, req.BackgroundVideo)
	if err != nil {
		return "", err
	}

	args := make([]string, 0, 32)
	args = append(args, "-y", "-nostdin", "-v", "error", "-filter_threads", "1", "-threads", "2")
	if bgPath == "" {
		args = append(args,
			"-f", "lavfi",
			"-i", fmt.Sprintf("color=c=black:s=%dx%d:r=30", width, height),
		)
	} else {
		args = append(args, "-stream_loop", "-1", "-i", bgPath)
	}
	args = append(args, "-i", req.AudioPath)

	audioDuration, durErr := b.probeDurationSeconds(ctx, req.AudioPath)
	if durErr == nil && audioDuration > maxOutputDurationSeconds {
		speed := audioDuration / maxOutputDurationSeconds
		filter := fmt.Sprintf("%s,atrim=duration=%.3f", atempoFilter(speed), maxOutputDurationSeconds)
		args = append(args, "-af", filter)
	}

	if bgPath != "" {
		if strings.EqualFold(strings.TrimSpace(req.Orientation), "original") {
			args = append(args,
				"-map", "0:v:0",
				"-map", "1:a:0",
			)
		} else {
			args = append(args,
				"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", width, height, width, height),
				"-map", "0:v:0",
				"-map", "1:a:0",
			)
		}
	}

	args = append(args,
		"-shortest",
		"-t", fmt.Sprintf("%.3f", maxOutputDurationSeconds),
		"-c:v", "libx264",
		"-preset", "veryfast", "-crf", "22", "-threads", "2",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-movflags", "+faststart",
		outPath,
	)

	cmd := exec.CommandContext(ctx, b.bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return outPath, nil
}

const maxOutputDurationSeconds = 55.0

func (b *FFmpegBuilder) probeDurationSeconds(ctx context.Context, mediaPath string) (float64, error) {
	cmd := exec.CommandContext(
		ctx,
		ffprobeBinaryFromFFmpeg(b.bin),
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		mediaPath,
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	durationRaw := strings.TrimSpace(stdout.String())
	duration, err := strconv.ParseFloat(durationRaw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse ffprobe duration %q: %w", durationRaw, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("invalid duration %.3f", duration)
	}

	return duration, nil
}

func ffprobeBinaryFromFFmpeg(ffmpegBin string) string {
	clean := strings.TrimSpace(ffmpegBin)
	if clean == "" || clean == "ffmpeg" {
		return "ffprobe"
	}
	base := filepath.Base(clean)
	if strings.Contains(strings.ToLower(base), "ffmpeg") {
		return filepath.Join(filepath.Dir(clean), strings.Replace(base, "ffmpeg", "ffprobe", 1))
	}
	return "ffprobe"
}

func atempoFilter(speed float64) string {
	if speed <= 1 {
		return "atempo=1.0"
	}

	remaining := speed
	parts := make([]string, 0, 4)
	for remaining > 2.0 {
		parts = append(parts, "atempo=2.0")
		remaining /= 2.0
	}
	if remaining < 0.5 {
		remaining = 0.5
	}
	remaining = math.Round(remaining*1000) / 1000
	parts = append(parts, fmt.Sprintf("atempo=%.3f", remaining))

	return strings.Join(parts, ",")
}

func resolveSize(orientation string, customWidth, customHeight int) (int, int) {
	switch strings.ToLower(strings.TrimSpace(orientation)) {
	case "landscape":
		return 1920, 1080
	case "square":
		return 1080, 1080
	case "custom":
		if customWidth > 0 && customHeight > 0 {
			return customWidth, customHeight
		}
		return 1080, 1920
	case "original":
		return 1080, 1920
	default:
		return 1080, 1920
	}
}

func resolveBackground(inputDir, selected string) (string, error) {
	inputDir = strings.TrimSpace(inputDir)
	if inputDir == "" {
		return "", nil
	}
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return "", fmt.Errorf("create input videos dir: %w", err)
	}

	if strings.TrimSpace(selected) != "" {
		rel := filepath.Clean(strings.TrimSpace(selected))
		if rel != "." && rel != string(filepath.Separator) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			p := filepath.Join(inputDir, rel)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	entries := make([]string, 0, 16)
	err := filepath.WalkDir(inputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".mp4", ".mov", ".mkv", ".webm":
			entries = append(entries, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("scan input videos: %w", err)
	}
	if len(entries) == 0 {
		return "", nil
	}
	sort.Strings(entries)
	for _, p := range entries {
		if strings.HasPrefix(strings.ToLower(filepath.Base(p)), "default_") {
			return p, nil
		}
	}
	return entries[0], nil
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
	return s
}
