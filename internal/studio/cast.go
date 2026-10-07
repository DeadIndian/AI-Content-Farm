package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type CastUpload struct {
	ID       string
	Name     string
	SpeakerA string
	SpeakerB string
	RoleA    string
	RoleB    string
	Files    map[string]io.Reader
}

type castSpeaker struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Idle  string `json:"idle"`
	Talk  string `json:"talk,omitempty"`
	Blink string `json:"blink,omitempty"`
}

type castManifest struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Speakers []castSpeaker `json:"speakers"`
}

func castText(value, field string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > limit || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%s must contain 1 to %d characters", field, limit)
	}
	return value, nil
}

func (s *Service) InstallCast(ctx context.Context, upload CastUpload) (map[string]any, error) {
	ctx, finish, err := s.operation(ctx, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer finish()
	if !ValidCastID(upload.ID) {
		return nil, fmt.Errorf("cast ID must be a lowercase slug of at most 64 characters")
	}
	for _, reserved := range []string{"nova-atlas", "cog-axiom", "ryusui-senku", "ryusui-sai"} {
		if upload.ID == reserved {
			return nil, fmt.Errorf("this cast ID is reserved; choose a new ID")
		}
	}
	name, err := castText(upload.Name, "cast name", 80)
	if err != nil {
		return nil, err
	}
	a, err := castText(upload.SpeakerA, "first speaker name", 60)
	if err != nil {
		return nil, err
	}
	b, err := castText(upload.SpeakerB, "second speaker name", 60)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(a, b) {
		return nil, fmt.Errorf("the two speakers need distinct names")
	}
	roles := []string{upload.RoleA, upload.RoleB}
	for i := range roles {
		if strings.TrimSpace(roles[i]) == "" {
			roles[i] = "Conversational educational presenter."
		}
		roles[i], err = castText(roles[i], "speaker role", 400)
		if err != nil {
			return nil, err
		}
	}
	if upload.Files["idle_a"] == nil || upload.Files["idle_b"] == nil {
		return nil, fmt.Errorf("provide an idle PNG for each speaker")
	}
	if err := os.MkdirAll(s.castRoot, 0755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(s.castRoot, ".upload-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	manifest := castManifest{ID: upload.ID, Name: name, Speakers: []castSpeaker{{ID: "speaker-a", Name: a, Role: roles[0], Idle: "idle_a.png"}, {ID: "speaker-b", Name: b, Role: roles[1], Idle: "idle_b.png"}}}
	var total int64
	for _, field := range []string{"idle_a", "idle_b", "talk_a", "talk_b", "blink_a", "blink_b"} {
		reader := upload.Files[field]
		if reader == nil {
			continue
		}
		path := filepath.Join(stage, field+".png")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		n, copyErr := io.Copy(file, io.LimitReader(reader, 4*1024*1024+1))
		total += n
		if copyErr != nil {
			file.Close()
			return nil, copyErr
		}
		if n > 4*1024*1024 || total > 16*1024*1024 {
			file.Close()
			return nil, fmt.Errorf("sprites must be at most 4 MiB each and 16 MiB in total")
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return nil, err
		}
		config, decodeErr := png.DecodeConfig(file)
		file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("%s must be a valid PNG image", field)
		}
		if config.Width < 32 || config.Height < 32 || config.Width > 4096 || config.Height > 4096 {
			return nil, fmt.Errorf("%s dimensions must be between 32 and 4096 pixels per side", field)
		}
		index := 0
		if strings.HasSuffix(field, "_b") {
			index = 1
		}
		if strings.HasPrefix(field, "talk_") {
			manifest.Speakers[index].Talk = field + ".png"
		}
		if strings.HasPrefix(field, "blink_") {
			manifest.Speakers[index].Blink = field + ".png"
		}
	}
	raw, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(stage, "cast.json")
	if err := os.WriteFile(manifestPath, raw, 0600); err != nil {
		return nil, err
	}
	cmd := s.workerCommand(ctx, env("STUDIO_CAST_SCRIPT", "scripts/studio_cast.py"), "--validate", manifestPath)
	var stderr tailBuffer
	cmd.Stderr = &stderr
	checked, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sprite validation failed: %s", strings.TrimSpace(string(stderr.data)))
	}
	var normalized castManifest
	if json.Unmarshal(checked, &normalized) != nil || normalized.ID != manifest.ID || len(normalized.Speakers) != 2 {
		return nil, fmt.Errorf("sprite validator returned an invalid manifest")
	}
	if err := os.WriteFile(manifestPath, checked, 0600); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.castMu.Lock()
	// Linux renameat2 publishes the entire validated directory atomically and
	// refuses to overwrite an existing custom cast, even under concurrent upload.
	err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, filepath.Join(s.castRoot, upload.ID), unix.RENAME_NOREPLACE)
	s.castMu.Unlock()
	if err == unix.EEXIST {
		return nil, fmt.Errorf("a cast with this ID already exists; choose a new ID")
	}
	if err != nil {
		return nil, err
	}
	characters := make([]map[string]any, 0, 2)
	urls := make([]string, 0, 2)
	for _, speaker := range normalized.Speakers {
		record := map[string]any{"id": speaker.ID, "name": speaker.Name, "role": speaker.Role, "idle": speaker.Idle}
		if speaker.Talk != "" {
			record["talk"] = speaker.Talk
		}
		if speaker.Blink != "" {
			record["blink"] = speaker.Blink
		}
		characters = append(characters, record)
		urls = append(urls, "/api/studio/cast/"+upload.ID+"/"+speaker.Idle)
	}
	return map[string]any{"id": upload.ID, "name": name, "speakers": []string{a, b}, "characters": characters, "available": true, "builtin": false, "reason": "", "sprite_urls": urls}, nil
}

func AvailablePair(caps map[string]any, id string) bool {
	pairs, ok := caps["pairs"].([]any)
	if !ok {
		return false
	}
	for _, raw := range pairs {
		pair, ok := raw.(map[string]any)
		if ok && pair["id"] == id {
			return pair["available"] == true
		}
	}
	return false
}

func AvailableVoice(caps map[string]any, id string) bool {
	renderer, ok := caps["renderer"].(map[string]any)
	if !ok {
		return false
	}
	providers, ok := renderer["voice_providers"].([]any)
	if !ok {
		return false
	}
	for _, raw := range providers {
		provider, ok := raw.(map[string]any)
		if ok && provider["id"] == id {
			return provider["available"] == true
		}
	}
	return false
}

func (s *Service) OpenCastSprite(id, name string) (*os.File, error) {
	if !ValidCastID(id) || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".png") {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(s.castRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Lstat(id)
	if err != nil || st.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrNotExist
	}
	manifestFile, err := root.Open(filepath.Join(id, "cast.json"))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(manifestFile, 32769))
	manifestFile.Close()
	if err != nil || len(raw) > 32768 {
		return nil, os.ErrNotExist
	}
	var manifest castManifest
	if json.NewDecoder(bytes.NewReader(raw)).Decode(&manifest) != nil || manifest.ID != id {
		return nil, os.ErrNotExist
	}
	allowed := false
	for _, speaker := range manifest.Speakers {
		if name == speaker.Idle || name == speaker.Talk || name == speaker.Blink {
			allowed = true
		}
	}
	if !allowed {
		return nil, os.ErrNotExist
	}
	file, err := root.Open(filepath.Join(id, name))
	if err != nil {
		return nil, err
	}
	st, err = file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}
