package loom

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// All executables, repositories and homes are synthetic; no downloads or GPU needed.
func repairFixture(t *testing.T, devices, cache string) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("POSIX fake toolchain with Linux CUDA detection")
	}
	testHome(t)
	if err := setEngineNode(nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOOM_SERVICE", "fixture-engine")
	tools := t.TempDir()
	repo := defaultRepoDir()
	put := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	put(filepath.Join(tools, "git"), "#!/bin/sh\ncase \"$*\" in\n'rev-parse --abbrev-ref HEAD') echo master;;\n'rev-parse --short HEAD'|'rev-parse --short origin/master') echo abc123;;\n'rev-parse HEAD'|'rev-parse origin/master') echo abc123full;;\n'rev-list --count HEAD..origin/master') echo 0;;\nesac\n")
	put(filepath.Join(tools, "nvcc"), "#!/bin/sh\nexit 0\n")
	put(filepath.Join(tools, "nvidia-smi"), "#!/bin/sh\nif [ \"$1\" = -L ]; then echo 'GPU 0: Fake NVIDIA'; else echo 8.6; fi\n")
	put(filepath.Join(tools, "systemctl"), "#!/bin/sh\nexit 1\n")
	// Configure must see an empty build tree, including on the CPU-only binary
	// case where CMakeCache already claims CUDA=ON.
	put(filepath.Join(tools, "cmake"), "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$*\" >> cmake.calls\nif [ \"$1\" = -B ]; then\n test ! -e build/repair-sentinel\n mkdir -p build/bin\n echo 'GGML_CUDA:BOOL=ON' > build/CMakeCache.txt\nelse\n cp "+shellQuote(filepath.Join(tools, "healthy-server"))+" build/bin/llama-server\n chmod +x build/bin/llama-server\nfi\n")
	put(filepath.Join(tools, "healthy-server"), "#!/bin/sh\necho 'Available devices:'\necho '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'\n")
	bin := filepath.Join(repo, "build", "bin", "llama-server")
	put(bin, "#!/bin/sh\necho 'Available devices:'\n"+devices+"\n")
	put(filepath.Join(repo, "build", "CMakeCache.txt"), cache)
	put(filepath.Join(repo, "build", "repair-sentinel"), "old build")
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldPlan := detectBuildPlan
	detectBuildPlan = func() buildPlan {
		return buildPlan{backend: "cuda", cudaCXX: filepath.Join(tools, "nvcc"), flags: append([]string{"-DGGML_CUDA=ON"}, cudaHostCompilerFlags(filepath.Join(tools, "nvcc"))...), jobs: 1}
	}
	t.Cleanup(func() { detectBuildPlan = oldPlan })
	if err := SetConfigKey("BIN", bin); err != nil {
		t.Fatal(err)
	}
	old := lcCur
	lcCur = &lcJob{Action: "update"}
	t.Cleanup(func() { lcCur = old; setBuildSink(nil) })
	return repo, bin
}

func TestEngineUpdateRepairsCurrentCommit(t *testing.T) {
	for _, mode := range []string{"web", "cli", "node"} {
		for _, state := range []string{"healthy", "cpu-only", "cache-mismatch", "missing"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				devices := "echo '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'"
				cache := "GGML_CUDA:BOOL=ON\n"
				if state == "cpu-only" {
					devices = "true"
				}
				if state == "cache-mismatch" {
					cache = "GGML_CUDA:BOOL=OFF\n"
				}
				repo, bin := repairFixture(t, devices, cache)
				if state == "missing" {
					if err := os.Remove(bin); err != nil {
						t.Fatal(err)
					}
				}
				switch mode {
				case "web":
					lcRunUpdate(false)
				case "cli":
					if err := llamacppUpdate([]string{"--no-restart"}); err != nil {
						t.Fatal(err)
					}
				case "node":
					n := &engineNode{URL: "http://fixture-node", WebKey: "fixture-key", Role: "engine-node"}
					mux := newEngineWorkerMux(n.WebKey)
					oldTransport := http.DefaultTransport
					http.DefaultTransport = discussionTransport(func(r *http.Request) (*http.Response, error) {
						if err := setEngineNode(nil); err != nil {
							t.Fatal(err)
						}
						w := httptest.NewRecorder()
						mux.ServeHTTP(w, r)
						return w.Result(), nil
					})
					defer func() { http.DefaultTransport = oldTransport }()
					if err := setEngineNode(n); err != nil {
						t.Fatal(err)
					}
					r := httptest.NewRequest("POST", "/api/llamacpp/update", strings.NewReader(`{"clean":false}`))
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					nodeAware(r.URL.Path, handleLlamacppUpdate)(w, r)
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
					deadline := time.Now().Add(10 * time.Second)
					for {
						lcMu.Lock()
						running := lcCur.Running
						lcMu.Unlock()
						if !running {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("update did not finish")
						}
						time.Sleep(time.Millisecond)
					}
					if err := setEngineNode(nil); err != nil {
						t.Fatal(err)
					}
				}
				if mode != "cli" && lcCur.Err != "" {
					t.Fatal(lcCur.Err)
				}
				calls, err := os.ReadFile(filepath.Join(repo, "cmake.calls"))
				if state == "healthy" {
					if err == nil {
						t.Fatalf("healthy current binary rebuilt: %s", calls)
					}
					return
				}
				if err != nil || !strings.Contains(string(calls), "--build") {
					t.Fatalf("unhealthy current binary was not rebuilt: %s %v", calls, err)
				}
				if !isFile(filepath.Join(repo, "build.old", "repair-sentinel")) {
					t.Fatal("repair did not clean the old tree")
				}
				if err := verifyGPUBuild(bin, "cuda"); err != nil {
					t.Fatal(err)
				}
				if mode != "cli" && state == "cpu-only" && !strings.Contains(lcCur.Phase, "rebuilt to restore GPU support") {
					t.Fatal(lcCur.Phase)
				}
			})
		}
	}
}

func TestVLLMUpdateDetectsBrokenEnvironment(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			testHome(t)
			t.Setenv("HOME", t.TempDir())
			bin := t.TempDir()
			write := func(path, body string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0755); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "uv"), "#!/bin/sh\nexit 0\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			write(vllmBin(), "#!/bin/sh\nexit 0\n")
			healthOutput := "echo 'GPU environment verified'"
			if broken {
				healthOutput = "echo 'no GPU devices available'; exit 1"
			}
			write(filepath.Join(vllmDir(), "bin", "python"), "#!/bin/sh\ncase \"$*\" in\n*importlib.metadata*) echo 1.0;;\n*) "+healthOutput+";;\nesac\n")
			v := &vllmState{}
			err := v.installOrUpdate(true)
			if broken && (err == nil || !strings.Contains(v.err, "no GPU")) {
				t.Fatalf("broken environment accepted: %v %s", err, v.err)
			}
			if !broken && err != nil {
				t.Fatal(err)
			}
			old := vllm
			vllm = v
			defer func() { vllm = old }()
			if err := putStoreJSON(bkState, vllmAutoState, engineAuto{Auto: true}); err != nil {
				t.Fatal(err)
			}
			vllmAutoTick(time.Now(), func(context.Context) (string, error) { return "1.0", nil })
			if broken && !strings.Contains(loadVLLMAuto().LastError, "no GPU") {
				t.Fatal("same-version auto check concealed broken GPU environment")
			}
		})
	}
}

func TestNodeWorkerManagesLlamaWhileVLLMLinkActive(t *testing.T) {
	_, _ = repairFixture(t, "true", "GGML_CUDA:BOOL=ON\n")
	if err := setEngineNode(&engineNode{Direct: true, Kind: "vllm"}); err != nil {
		t.Fatal(err)
	}
	defer setEngineNode(nil)
	mux := newEngineWorkerMux("fixture-key")
	for _, path := range []string{"/api/llamacpp", "/api/backends/custom"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer fixture-key")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestPrebuiltUpdateRepairsCurrentRelease(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			repairFixture(t, "true", "GGML_CUDA:BOOL=ON\n")
			body := "#!/bin/sh\necho 'Available devices:'\necho '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'\n"
			bin := filepath.Join(prebuiltDir(), "llama-b999", "llama-server")
			if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
				t.Fatal(err)
			}
			existing := body
			if !healthy {
				existing = "#!/bin/sh\necho 'Available devices:'\n"
			}
			if err := os.WriteFile(bin, []byte(existing), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(prebuiltDir(), "VERSION"), []byte("b999 12.8 fmt2\n"), 0644); err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(&tar.Header{Name: "llama-b999/llama-server", Mode: 0755, Size: int64(len(body))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			oldTransport := http.DefaultTransport
			defer func() { http.DefaultTransport = oldTransport }()
			downloads := 0
			http.DefaultTransport = discussionTransport(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				switch r.URL.Host {
				case "api.github.com":
					fmt.Fprintf(w, `[{"tag_name":"b999","assets":[{"name":"llama-b999-bin-ubuntu-cuda-12.8-%s.tar.gz","browser_download_url":"https://fixture/archive"}]}]`, map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH])
				case "fixture":
					downloads++
					w.Write(archive.Bytes())
				default:
					t.Fatal("unexpected network", r.URL)
				}
				return w.Result(), nil
			})
			_, err := prebuiltInstall(func(string) {}, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			if healthy && downloads != 0 {
				t.Fatal("healthy release reinstalled")
			}
			if !healthy && downloads != 1 {
				t.Fatal("unhealthy current release was not reinstalled")
			}
			if err := verifyGPUBuild(bin, "cuda"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCustomBackendRepairs(t *testing.T) {
	repo, _ := repairFixture(t, "true", "GGML_CUDA:BOOL=ON\n")
	custom := filepath.Join(backendsDir(), "fixture-custom")
	if err := os.Rename(repo, custom); err != nil {
		t.Fatal(err)
	}
	bin, err := installCustomBackend("https://fixture/custom.git", "fixture-custom", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !isFile(filepath.Join(custom, "build.old", "repair-sentinel")) {
		t.Fatal("custom repair reused unhealthy build")
	}
	if err := verifyGPUBuild(bin, "cuda"); err != nil {
		t.Fatal(err)
	}
}

func TestCustomBackendRejectsCPUOnlyBuild(t *testing.T) {
	repo, _ := repairFixture(t, "echo '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'", "GGML_CUDA:BOOL=ON\n")
	custom := filepath.Join(backendsDir(), "fixture-custom")
	if err := os.Rename(repo, custom); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(custom, "build", "repair-sentinel")); err != nil {
		t.Fatal(err)
	}
	// A compiler that keeps producing a CPU-only server must fail the job.
	cmake, _ := exec.LookPath("cmake")
	if err := os.WriteFile(filepath.Join(filepath.Dir(cmake), "healthy-server"), []byte("#!/bin/sh\necho 'Available devices:'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := installCustomBackend("https://fixture/custom.git", "fixture-custom", "", nil); err == nil {
		t.Fatal("custom CPU-only build reported success")
	}
}

func TestExistingInstallRepairsRequestedCheckout(t *testing.T) {
	for _, mode := range []string{"web", "cli"} {
		t.Run(mode, func(t *testing.T) {
			repo, _ := repairFixture(t, "true", "GGML_CUDA:BOOL=ON\n")
			requested := filepath.Join(backendsDir(), "requested-checkout")
			if err := os.Rename(repo, requested); err != nil {
				t.Fatal(err)
			}
			if mode == "web" {
				lcRunInstall(false, requested)
				if lcCur.Err != "" {
					t.Fatal(lcCur.Err)
				}
			} else {
				if err := llamacppInstall([]string{"--dir=" + requested}); err != nil {
					t.Fatal(err)
				}
			}
			if !isFile(filepath.Join(requested, "build.old", "repair-sentinel")) {
				t.Fatal("existing requested checkout not repaired")
			}
			if !samePath(ReadConfig()["BIN"], llamaServerBin(requested)) {
				t.Fatal("wrong checkout linked")
			}
		})
	}
}

func TestVerifyGPUBuildRejectsFailedDeviceCommand(t *testing.T) {
	_, bin := repairFixture(t, "echo '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'; exit 1", "GGML_CUDA:BOOL=ON\n")
	for _, backend := range []string{"cuda", "cpu"} {
		if err := verifyGPUBuild(bin, backend); err == nil {
			t.Fatalf("%s accepted a failed device command", backend)
		}
	}
}

func TestEngineHealthStatusAndCheck(t *testing.T) {
	for _, state := range []string{"healthy", "cpu-only", "missing", "probe-error"} {
		t.Run(state, func(t *testing.T) {
			devices := "echo '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'"
			if state == "cpu-only" {
				devices = "true"
			}
			if state == "probe-error" {
				devices = "exit 1"
			}
			_, bin := repairFixture(t, devices, "GGML_CUDA:BOOL=ON\n")
			if state == "missing" {
				if err := os.Remove(bin); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"/api/llamacpp", "/api/llamacpp/check"} {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", path, nil)
				if strings.HasSuffix(path, "/check") {
					handleLlamacppCheck(w, r)
				} else {
					handleLlamacpp(w, r)
				}
				var status struct {
					Health struct {
						Healthy  bool
						Observed bool
						Devices  []map[string]any
					}
					CanUpdate    bool `json:"can_update"`
					NeedsRebuild bool `json:"needs_rebuild"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
					t.Fatal(err)
				}
				if status.Health.Healthy != (state == "healthy") {
					t.Fatalf("%s %s", path, w.Body.String())
				}
				if status.Health.Observed != (state == "healthy" || state == "cpu-only") {
					t.Fatal(w.Body.String())
				}
				if path == "/api/llamacpp" && !status.CanUpdate {
					t.Fatal("missing source binary is not repairable")
				}
				if strings.HasSuffix(path, "/check") && status.NeedsRebuild == (state == "healthy") {
					t.Fatal("wrong rebuild advice", w.Body.String())
				}
			}
		})
	}
}

func TestSourceAutoUpdateRecognizesCurrentCommitRepair(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			devices := "true"
			if healthy {
				devices = "echo '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'"
			}
			repairFixture(t, devices, "GGML_CUDA:BOOL=ON\n")
			kind, from, to, err := engineUpdateAvailable()
			if err != nil || kind != "source" || from != "abc123" {
				t.Fatal(kind, from, to, err)
			}
			if healthy && to != "" || !healthy && to != "abc123" {
				t.Fatal("wrong repair availability", to)
			}
		})
	}
}

func TestCleanRebuildInvalidatesOldDeviceLists(t *testing.T) {
	_, bin := repairFixture(t, "echo '  CUDA0: Fake NVIDIA (16000 MiB, 15000 MiB free)'", "GGML_CUDA:BOOL=ON\n")
	key := bin + "\x00"
	stale := []map[string]any{{"id": "Vulkan0", "name": "old acceleration"}}
	devCachePut(key, stale)
	devPersistPut(key, stale)
	lcRunUpdate(true)
	if lcCur.Err != "" {
		t.Fatal(lcCur.Err)
	}
	if _, ok := devCacheGet(key); ok {
		t.Fatal("old memory device list survived replacement")
	}
	if _, ok := devPersistGet(key); ok {
		t.Fatal("old persistent device list survived replacement")
	}
}
