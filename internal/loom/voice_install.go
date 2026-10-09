package loom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type voiceJobState struct {
	Running  bool   `json:"running"`
	Phase    string `json:"phase"`
	Artifact string `json:"artifact"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Error    string `json:"error"`
}
type voiceDownloadJob struct {
	mu     sync.Mutex
	State  voiceJobState
	cancel context.CancelFunc
}

var voiceJob = &voiceDownloadJob{}
var voiceHTTPClient = &http.Client{Timeout: 0}
var voiceNVIDIA = func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func (j *voiceDownloadJob) snapshot() voiceJobState { j.mu.Lock(); defer j.mu.Unlock(); return j.State }
func (j *voiceDownloadJob) progress(phase, id string, n, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.State.Phase, j.State.Artifact, j.State.Received, j.State.Total = phase, id, n, total
}
func (j *voiceDownloadJob) begin(run func(context.Context) error) error {
	voiceRuntime.mu.Lock()
	defer voiceRuntime.mu.Unlock()
	if voiceRuntime.busyLocked() {
		return errors.New("stop voice before starting an installation")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State.Running {
		return errors.New("a voice download is already running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	j.cancel = cancel
	j.State = voiceJobState{Running: true, Phase: "queued"}
	go func() {
		err := run(ctx)
		cancel()
		j.mu.Lock()
		defer j.mu.Unlock()
		j.State.Running = false
		j.State.Phase = "complete"
		if err != nil {
			j.State.Phase = "failed"
			j.State.Error = err.Error()
		}
		if errors.Is(err, context.Canceled) {
			j.State.Phase = "cancelled"
		}
		j.cancel = nil
	}()
	return nil
}
func (j *voiceDownloadJob) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
}

// No trust-on-first-use: size is a transport check, the embedded hash is the
// authority. Refuse before requesting any bytes if a reviewer has not pinned it.
func voiceDownload(ctx context.Context, a voiceArtifact, dest string, progress func(int64)) error {
	if !voiceHashReady(a) {
		return fmt.Errorf("verification_unavailable: pin the independently verified SHA256 for %s in voice/catalog.json before installing", a.ID)
	}
	if a.Size <= 0 {
		return errors.New("voice archive has no verified release size")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	resp, err := voiceHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("voice download HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != a.Size {
		return errors.New("voice archive size mismatch")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	dst := io.MultiWriter(f, h)
	buf := make([]byte, 128<<10)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, e := resp.Body.Read(buf)
		if read > 0 {
			n += int64(read)
			if n > a.Size {
				return errors.New("voice archive exceeds release size")
			}
			if _, err := dst.Write(buf[:read]); err != nil {
				return err
			}
			progress(n)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if n != a.Size {
		return errors.New("truncated voice archive")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return errors.New("voice archive SHA256 mismatch")
	}
	return f.Sync()
}

// Other platform sizes/names are read from the official release metadata only
// after the corresponding embedded hash is pinned. No latest-version drift.
func voiceResolveEngine(ctx context.Context, a voiceArtifact) (voiceArtifact, error) {
	if !voiceHashReady(a) {
		return a, fmt.Errorf("verification_unavailable: SHA256 TODO for %s", a.ID)
	}
	if a.URL != "" && a.Size > 0 {
		return a, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/k2-fsa/sherpa-onnx/releases/tags/v"+curatedVoice.Version, nil)
	resp, err := voiceHTTPClient.Do(req)
	if err != nil {
		return a, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return a, errors.New("sherpa release metadata unavailable")
	}
	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return a, err
	}
	osName := map[string]string{"linux": "linux", "darwin": "osx", "windows": "win"}[a.OS]
	arch := map[string]string{"amd64": "x64", "arm64": "aarch64"}[a.Arch]
	if a.OS != "linux" && a.Arch == "arm64" {
		arch = "arm64"
	}
	var matches []voiceArtifact
	for _, item := range release.Assets {
		n := strings.ToLower(item.Name)
		if !strings.HasPrefix(n, "sherpa-onnx-v"+curatedVoice.Version+"-") || !voiceEnginePlatformMatch(n, osName, arch) || !strings.Contains(n, "shared") || strings.Contains(n, "cuda") != (a.Provider == "cuda") || strings.Contains(n, "python") {
			continue
		}
		if !strings.HasSuffix(n, ".tar.bz2") && !strings.HasSuffix(n, ".zip") {
			continue
		}
		candidate := a
		candidate.Archive = item.Name
		candidate.URL = item.URL
		candidate.Size = item.Size
		matches = append(matches, candidate)
	}
	if len(matches) != 1 {
		return a, fmt.Errorf("expected one official sherpa asset for %s, found %d", a.ID, len(matches))
	}
	return matches[0], nil
}
func voiceFindBinary(root, name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var found []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && d.Name() == name {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(found) != 1 {
		return "", fmt.Errorf("sherpa binary %s not found or ambiguous", name)
	}
	return found[0], nil
}
func voiceCheckVersion(ctx context.Context, root string) (string, error) {
	bin, err := voiceFindBinary(root, "sherpa-onnx-version")
	if err != nil {
		return "", err
	}

	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, bin).CombinedOutput()
	if err != nil {
		return "", errors.New("sherpa version binary failed")
	}
	version := strings.TrimSpace(string(out))
	if !regexp.MustCompile(`(?m)^sherpa-onnx version\s*:\s*` + regexp.QuoteMeta(curatedVoice.Version) + `\s*$`).MatchString(version) {
		return "", errors.New("sherpa version does not match curated release")
	}
	return curatedVoice.Version, nil
}
func voiceInstallArtifact(ctx context.Context, a voiceArtifact, engine bool) error {
	if engine {
		var err error
		a, err = voiceResolveEngine(ctx, a)
		if err != nil {
			return err
		}
	}
	if !voiceHashReady(a) {
		return fmt.Errorf("verification_unavailable: SHA256 TODO for %s", a.ID)
	}
	if err := os.MkdirAll(voiceRoot(), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(voiceRoot(), ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, a.Archive)
	voiceJob.progress("downloading", a.ID, 0, a.Size)
	if err := voiceDownload(ctx, a, archive, func(n int64) { voiceJob.progress("downloading", a.ID, n, a.Size) }); err != nil {
		return err
	}
	voiceJob.progress("extracting", a.ID, a.Size, a.Size)
	extracted := filepath.Join(stage, "files")
	if err := os.Mkdir(extracted, 0700); err != nil {
		return err
	}
	if strings.HasSuffix(a.Archive, ".onnx") {
		if err := os.Rename(archive, filepath.Join(extracted, a.Archive)); err != nil {
			return err
		}
	} else if err := extractArchive(archive, extracted); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if engine {
		if _, err := voiceLibraryPath(extracted); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			bin, err := voiceFindBinary(extracted, "sherpa-onnx-version")
			if err != nil {
				return err
			}
			if err := os.Chmod(bin, 0755); err != nil {
				return err
			}
		}
		if _, err := voiceCheckVersion(ctx, extracted); err != nil {
			return err
		}
	} else {
		if _, err := voiceResolveFiles(extracted, a); err != nil {
			return err
		}
	}
	receipt, _ := json.Marshal(a)
	if err := os.WriteFile(filepath.Join(extracted, ".verified.json"), receipt, 0600); err != nil {
		return err
	}
	voiceRuntime.mu.Lock()
	defer voiceRuntime.mu.Unlock()
	if voiceRuntime.busyLocked() {
		return errors.New("voice service is active; stop it before installing")
	}
	oldEngine := readVoiceEngine()
	oldInstalled := voiceEngineInstalled()
	dest := voiceModelDir(a.ID)
	if engine {
		dest = filepath.Join(voiceRoot(), "sherpa-onnx", curatedVoice.Version, a.Provider)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	previous := dest + ".previous"
	// Retain the previous verified installation; never remove the working one
	// until extraction and native version/model validation have succeeded.
	if _, err := os.Stat(dest); err == nil {
		if err := os.RemoveAll(previous); err != nil {
			return err
		}
		if err := os.Rename(dest, previous); err != nil {
			return err
		}
	}
	if err := os.Rename(extracted, dest); err != nil {
		_ = os.Rename(previous, dest)
		return err
	}
	if engine {
		old := oldEngine
		next := voiceEngineSelection{Version: curatedVoice.Version, Provider: a.Provider}
		if oldInstalled {
			next.PreviousVersion, next.PreviousProvider = old.Version, old.Provider
			next.PreviousCopy = old.Version == next.Version && old.Provider == next.Provider
		}
		if err := putStoreJSON(bkState, "voice_engine_selection", next); err != nil {
			_ = os.RemoveAll(dest)
			_ = os.Rename(previous, dest)
			return err
		}
	}
	return nil
}
func voiceDeleteModel(id string) error {
	voiceRuntime.mu.Lock()
	defer voiceRuntime.mu.Unlock()
	if voiceRuntime.busyLocked() || voiceJob.snapshot().Running {
		return errors.New("stop voice and finish downloads before deleting models")
	}
	c := readVoiceConfig()
	if c.STT == id || c.TTS == id || c.VAD == id {
		return errors.New("deselect the active voice model before deleting it")
	}
	if err := os.RemoveAll(voiceModelDir(id)); err != nil {
		return err
	}
	return os.RemoveAll(voiceModelDir(id) + ".previous")
}
func voiceRollback(ctx context.Context) error {
	voiceRuntime.mu.Lock()
	defer voiceRuntime.mu.Unlock()
	if voiceRuntime.busyLocked() || voiceJob.snapshot().Running {
		return errors.New("stop voice and finish downloads before rollback")
	}
	selection := readVoiceEngine()
	if selection.PreviousVersion == "" || selection.PreviousProvider == "" {
		return errors.New("no previous verified voice engine")
	}
	previousSelection := voiceEngineSelection{Version: selection.PreviousVersion, Provider: selection.PreviousProvider}
	if !validVoiceEngineSelection(previousSelection) {
		return errors.New("invalid previous voice selection")
	}
	dest := voiceEngineDir()
	previous := filepath.Join(voiceRoot(), "sherpa-onnx", selection.PreviousVersion, selection.PreviousProvider)
	if selection.PreviousCopy {
		previous += ".previous"
	}
	if _, err := os.Stat(filepath.Join(previous, ".verified.json")); err != nil {
		return errors.New("previous engine receipt missing")
	}
	if _, err := voiceCheckVersion(ctx, previous); err != nil {
		return err
	}
	if selection.PreviousCopy {
		swapped := dest + ".swap"
		if err := os.Rename(dest, swapped); err != nil {
			return err
		}
		if err := os.Rename(previous, dest); err != nil {
			_ = os.Rename(swapped, dest)
			return err
		}
		if err := os.Rename(swapped, previous); err != nil {
			_ = os.Rename(dest, previous)
			_ = os.Rename(swapped, dest)
			return err
		}
	}
	next := previousSelection
	next.PreviousVersion, next.PreviousProvider, next.PreviousCopy = selection.Version, selection.Provider, selection.PreviousCopy
	if err := putStoreJSON(bkState, "voice_engine_selection", next); err != nil {
		if selection.PreviousCopy {
			swapped := dest + ".swap"
			_ = os.Rename(dest, swapped)
			_ = os.Rename(previous, dest)
			_ = os.Rename(swapped, previous)
		}
		return err
	}
	return nil
}

func voiceEnginePlatformMatch(name, goos, arch string) bool {
	if strings.Contains(name, goos+"-"+arch) {
		return true
	}
	if goos == "linux" && arch == "aarch64" {
		return strings.Contains(name, "linux-arm64")
	}
	if goos == "osx" {
		return strings.Contains(name, "osx-universal2") || strings.Contains(name, "macos-universal2") || strings.Contains(name, "macos-"+arch)
	}
	if goos == "win" {
		return strings.Contains(name, "windows-"+arch)
	}
	return false
}
