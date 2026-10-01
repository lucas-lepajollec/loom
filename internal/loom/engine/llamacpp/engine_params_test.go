package llamacpp

import (
	"testing"
)

const fakeRouterHelp = `----- common params -----

-c,    --ctx-size N                     size of the prompt context
-fa,   --flash-attn [on|off|auto]       set Flash Attention use
-ngl,  --gpu-layers, --n-gpu-layers N   max. number of layers to store in VRAM
-ctk,  --cache-type-k TYPE              KV cache data type for K
--mmap, --no-mmap                       whether to memory-map model
--host, --no-host                       bypass host buffer allowing extra buffers to be used
--reasoning-budget N                    token budget for thinking

----- example-specific params -----

-np,   --parallel N                     number of server slots
-m,    --model FNAME                    model path to load
--host HOST                             ip address to listen
--port PORT                             port to listen
--api-key KEY                           API key to use for authentication
--slots, --no-slots                     expose slots monitoring endpoint
--models-preset PATH                    path to INI file containing model presets for the router server
--models-max N                          for router server, maximum number of models to load simultaneously
`

func TestCuratedParamsSchemaAndInstalledHelp(t *testing.T) {
	c := ReadCuratedParams()
	seen := map[string]bool{}
	for _, p := range c.Params {
		if seen[p.ID] || p.ID == "" || p.Label == "" || p.Tip == "" || (p.Tier != "essential" && p.Tier != "advanced") {
			t.Fatalf("invalid curated control: %+v", p)
		}
		seen[p.ID] = true
		for _, choice := range p.Choices {
			if len(choice) != 2 {
				t.Fatalf("invalid choices for %s", p.ID)
			}
		}
		if p.RequiresFlag && p.Flag == "" {
			t.Fatalf("%s requires an unnamed flag", p.ID)
		}
	}
	flags := ParseLlamaHelp(fakeRouterHelp + "\n--fit [on|off]    fit memory\n--seed N    seed\n--obsolete N    deprecated\n")
	got := MergeEngineParams(c, flags)
	find := func(id string) ParamSpec {
		for _, p := range got {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("missing %s", id)
		return ParamSpec{}
	}
	if p := find("gpu-layers"); p.Flag != "--gpu-layers" || p.Key != "NGL" || !p.Supported {
		t.Fatalf("aliases not merged: %+v", p)
	}
	if p := find("fit"); p.Kind != "bool" || p.NativeKind != "enum" || !p.Available {
		t.Fatalf("fit switch lost native valued flag: %+v", p)
	}
	if find("batch-size").Available {
		t.Fatal("absent native flag became available")
	}
	if !find("sysprompt").Available || !find("kv").Available {
		t.Fatal("composite/model controls require installed flags")
	}
	if p := find("seed"); p.Tier != "expert" || p.Label != "--seed" || !p.Supported {
		t.Fatalf("missing help-driven expert: %+v", p)
	}
	for _, p := range got {
		if p.Tier == "expert" && (p.ID == "api-key" || p.ID == "model" || p.ID == "mmap" || p.ID == "obsolete") {
			t.Fatalf("hidden/deprecated/covered flag exposed: %s", p.ID)
		}
	}
}

func TestNewAdvancedParamNeedsOnlyCatalogData(t *testing.T) {
	c := ReadCuratedParams()
	// Simulates one JSON entry, not a backend switch or UI allowlist edit.
	c.Params = append(c.Params, ParamSpec{ID: "seed", Flag: "--seed", Label: "Graine", Tier: "advanced", Kind: "number", RequiresFlag: true})
	got := MergeEngineParams(c, ParseLlamaHelp("--seed N    random seed (default: -1)\n"))
	count := 0
	for _, p := range got {
		if p.ID == "seed" {
			count++
			if !p.Available || p.Tier != "advanced" || p.Default != "-1" {
				t.Fatalf("new control not resolved: %+v", p)
			}
		}
	}
	if count != 1 {
		t.Fatal("new curated flag also appears in Expert")
	}
}
