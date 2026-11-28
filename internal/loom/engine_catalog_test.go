package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCatalogEntriesAndHostCompatibility(t *testing.T) {
	wants := map[string]int{"llama.cpp": 8080, "ik_llama.cpp": 8080, "vllm": 8000, "sglang": 30000, "mlx": 8080, "ollama": 11434,
		"lmstudio": 1234, "tabbyapi": 5000, "koboldcpp": 5001, "lemonade": 8000, "strata": 8080, "basalt": 8080, "mistral.rs": 1234, "ktransformers": 10002, "exo": 52415, "openai": 0}
	items := engineCatalogEntries()
	if len(items) != len(wants) {
		t.Fatalf("got %d catalog entries", len(items))
	}
	for _, item := range items {
		if port, ok := wants[item.ID]; !ok || item.DefaultPort != port || item.Name == "" || !slices.Contains(item.LinkOptions, "url") {
			t.Fatalf("invalid entry: %+v", item)
		}
		switch item.ID {
		case "sglang", "vllm":
			catalogCompatibility(&item, catalogHost{OS: "linux", Arch: "amd64", Accelerators: []string{"CPU", "CUDA"}}, true, false, false)
			if !item.Compatible || item.RequirementNote != "" {
				t.Fatalf("GPU host: %+v", item)
			}
			catalogCompatibility(&item, catalogHost{OS: "windows", Arch: "amd64", Accelerators: []string{"CPU", "CUDA"}}, true, false, false)
			if item.Compatible || item.RequirementNote != "linux_gpu" {
				t.Fatalf("Windows host: %+v", item)
			}
		case "mlx":
			catalogCompatibility(&item, catalogHost{OS: "darwin", Arch: "arm64"}, true, false, false)
			if item.Category != "managed" || !item.Compatible || len(item.InstallOptions) != 1 {
				t.Fatalf("Apple Silicon: %+v", item)
			}
			catalogCompatibility(&item, catalogHost{OS: "linux", Arch: "arm64"}, true, false, false)
			if item.Category != "server" || !item.Compatible || len(item.InstallOptions) != 0 || item.RequirementNote != "apple_silicon" {
				t.Fatalf("external MLX: %+v", item)
			}
		case "ik_llama.cpp":
			if item.CustomName != "ik-llama-cpp" || item.SourceURL != "https://github.com/ikawrakow/ik_llama.cpp" {
				t.Fatalf("fork entry: %+v", item)
			}
		}
		delete(wants, item.ID)
	}
}

func TestCatalogDefaultPortProbesAreConcurrentAndBounded(t *testing.T) {
	started := make(chan struct{}, 3)
	unblock := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-unblock:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			io.WriteString(w, `{"version":"0.21.2"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer fast.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	items := []catalogEntry{{ID: "ollama", DefaultPort: 11434, Detect: true}, {ID: "lemonade", DefaultPort: 8000, Detect: true},
		{ID: "koboldcpp", DefaultPort: 5001, Detect: true}, {ID: "tabbyapi", DefaultPort: 5000, Detect: true}}
	done := make(chan []directEngine, 1)
	begin := time.Now()
	go func() {
		done <- probeCatalogServices(ctx, items, func(port int) string {
			if port == 11434 {
				return fast.URL
			}
			return slow.URL
		})
	}()
	for range 3 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("probes did not start concurrently")
		}
	}
	close(unblock)
	probes := <-done
	if time.Since(begin) > time.Second || probes[0].Kind != "ollama" || probes[0].Version != "0.21.2" || probes[1].Kind != "" {
		t.Fatalf("probe result = %+v", probes)
	}

	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stalled.Close()
	begin = time.Now()
	probeCatalogServices(ctx, items[1:], func(int) string { return stalled.URL })
	if time.Since(begin) > 750*time.Millisecond {
		t.Fatal("catalog probes exceeded the shared deadline")
	}
}

func TestCatalogServedByLoomAndNodeWithoutSecrets(t *testing.T) {
	testHome(t)
	if err := setEngineNode(&engineNode{Direct: true, Kind: "openai", Model: "model", V1: "http://127.0.0.1:1", APIKey: "catalog-native-secret"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		h     http.Handler
		token string
	}{
		{newWebMux(), ""}, {newEngineWorkerMux("catalog-management-secret"), "catalog-management-secret"},
	} {
		req := localTestRequest("GET", "/api/engines/catalog", nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		rec := httptest.NewRecorder()
		test.h.ServeHTTP(rec, req)
		var out struct {
			Engines     []catalogEntry
			Connections []engineConnection
			Active      activeCatalogEngine `json:"active_engine"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Engines) != 16 || len(out.Connections) != 1 ||
			out.Active.Kind != "openai" || !out.Connections[0].KeySet || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("catalog = %d %s", rec.Code, rec.Body)
		}
	}
}

func TestCatalogNewEngineGuidesAndBasaltHardware(t *testing.T) {
	want := map[string]struct{ category, format, capability string }{
		"strata":        {"runtime", ".strata", "expert_streaming"},
		"basalt":        {"runtime", ".basalt", "vision_offload"},
		"mistral.rs":    {"runtime", "ISQ", "vision_audio"},
		"ktransformers": {"server", "GGUF", "heterogeneous"},
		"exo":           {"server", "MLX", "lan_cluster"},
	}
	for _, item := range engineCatalogEntries() {
		expected, ok := want[item.ID]
		if !ok {
			continue
		}
		if item.Category != expected.category || !slices.Contains(item.Formats, expected.format) ||
			!slices.Contains(item.Capabilities, expected.capability) || !slices.Contains(item.InstallOptions, "guide") ||
			item.InstallHint == "" || !strings.HasPrefix(item.InstallGuide, "https://") {
			t.Errorf("new entry = %+v", item)
		}
		delete(want, item.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing engines: %v", want)
	}

	for _, tc := range []struct {
		name, os, arch, gpus, badge, guide, hint string
		compatible                               bool
	}{
		{"RTX 50", "linux", "amd64", "0, NVIDIA GeForce RTX 5090, 32768, 0, 12.0\n", "blackwell", basaltCUDAInstall, "basalt_cuda", true},
		{"DGX Spark", "linux", "arm64", "0, NVIDIA GB10, 65536, 0, 12.1\n", "blackwell", basaltCUDAInstall, "basalt_cuda", true},
		{"Mixed cards", "windows", "amd64", "0, NVIDIA GeForce RTX 4090, 24576, 0, 8.9\n1, NVIDIA GeForce RTX 5070, 12288, 0, 12.0\n", "blackwell", basaltCUDAInstall, "basalt_cuda", true},
		{"Older NVIDIA", "linux", "amd64", "0, NVIDIA GeForce RTX 4090, 24576, 0, 8.9\n", "", basaltCUDAInstall, "basalt_cuda", false},
		{"Data center Blackwell", "linux", "amd64", "0, NVIDIA B200, 180000, 0, 10.0\n", "", basaltCUDAInstall, "basalt_cuda", false},
		{"Apple Silicon", "darwin", "arm64", "", "", basaltMLXInstall, "basalt_mlx", true},
		{"CPU only", "linux", "amd64", "", "", basaltCUDAInstall, "basalt_cuda", false},
		{"Intel Mac", "darwin", "amd64", "", "", basaltCUDAInstall, "basalt_cuda", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := detectCatalogHostWithRunner(tc.os, tc.arch, func(name string, args ...string) ([]byte, error) {
				if name == "nvidia-smi" && strings.Contains(args[0], "compute_cap") && tc.gpus != "" {
					return []byte(tc.gpus), nil
				}
				return nil, errors.New("no device")
			})
			items := engineCatalogEntries()
			item := items[slices.IndexFunc(items, func(item catalogEntry) bool { return item.ID == "basalt" })]
			catalogCompatibility(&item, host, false, false, false)
			if item.Compatible != tc.compatible || item.HardwareBadge != tc.badge ||
				item.InstallGuide != tc.guide || item.InstallHint != tc.hint || !slices.Contains(item.LinkOptions, "url") {
				t.Fatalf("host = %+v, catalog = %+v", host, item)
			}
			if (item.RequirementNote == "basalt_hardware") == tc.compatible {
				t.Fatalf("wrong local requirement: %q", item.RequirementNote)
			}
			if tc.hint == "basalt_mlx" && !slices.Equal(item.Formats, []string{"MLX", "GGUF"}) {
				t.Fatalf("macOS formats = %v", item.Formats)
			}
		})
	}
}

func TestCatalogDetectsExoOnItsLoopbackPort(t *testing.T) {
	items := []catalogEntry{{ID: "exo", DefaultPort: 52415, Detect: true}}
	for _, tc := range []struct {
		name, models, state, kind string
	}{
		{"OpenAI model owner", `{"data":[{"id":"mlx-community/model","owned_by":"exo"}]}`, "", "exo"},
		{"Empty cluster", `{"data":[]}`, `{"topology":{"nodes":[]},"instances":{},"runners":{}}`, "exo"},
		{"Generic server", `{"data":[{"id":"model"}]}`, `{"status":"ready"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/models":
					io.WriteString(w, tc.models)
				case r.URL.Path == "/state" && tc.state != "":
					io.WriteString(w, tc.state)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			probes := probeCatalogServices(context.Background(), items, func(port int) string {
				if port != 52415 {
					t.Errorf("probed port %d", port)
				}
				return server.URL
			})
			if probes[0].Kind != tc.kind {
				t.Fatalf("exo scan: %+v", probes[0])
			}
		})
	}
}
