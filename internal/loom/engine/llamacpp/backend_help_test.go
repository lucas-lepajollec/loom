package llamacpp

import (
	"testing"
)

func TestParseLlamaHelpCoreFlags(t *testing.T) {
	const help = `----- common params -----

-h,    --help, --usage                  print usage and exit
-c,    --ctx-size N                     size of the prompt context (default: 0, 0 = loaded from model)
-fa,   --flash-attn [on|off|auto]       set Flash Attention use ('on', 'off', or 'auto', default: 'auto')
-ngl,  --gpu-layers, --n-gpu-layers N   max. number of layers to store in VRAM
--mlock                                 DEPRECATED in favor of ` + "`--load-mode`" + `: force system to keep model in RAM
-lm,   --load-mode MODE                 model loading mode (default: auto)
                                        - auto: mmap, unless a device does not support it
                                        - none: no special loading mode
                                        - mmap: memory-map model
                                        - mlock: force system to keep model in RAM
--swa-full                              use full-size SWA cache (default: false)

----- sampling params -----

--temp, --temperature N                 temperature (default: 0.80)
--top-p N                               top-p sampling (default: 0.95, 1.0 = disabled)

----- example-specific params -----

-np,   --parallel N                     number of server slots (default: -1, -1 = auto)
-mm,   --mmproj FILE                    path to a multimodal projector file
--mmproj-auto, --no-mmproj, --no-mmproj-auto
                                        whether to try loading a multimodal projector automatically
-rea,  --reasoning [on|off|auto]        Use reasoning/thinking in the chat ('on', 'off', or 'auto', default: 'auto')
--reasoning-effort LEVEL                reasoning effort level given to the chat template
--spec-type none,draft-simple,draft-eagle3,draft-mtp
                                        comma-separated list of types of speculative decoding to use (default: none)
--spec-draft-model, -md, --model-draft FNAME
                                        draft model for speculative decoding (default: unused)
-m,    --model FNAME                    model path to load
--host HOST                             ip address to listen
--port PORT                             port to listen (default: 8080)
`
	flags := ParseLlamaHelp(help)
	byID := map[string]LlamaFlag{}
	for _, f := range flags {
		byID[f.ID] = f
	}
	must := []string{"ctx-size", "flash-attn", "gpu-layers", "load-mode", "temp", "parallel", "mmproj", "mmproj-auto", "reasoning", "reasoning-effort", "swa-full"}
	for _, id := range must {
		if _, ok := byID[id]; !ok {
			t.Fatalf("flag %s introuvable : %+v", id, keysOf(byID))
		}
	}
	ctx := byID["ctx-size"]
	if ctx.Kind != "int" || ctx.Key != "CTX" || ctx.Tier != "base" {
		t.Fatalf("ctx-size mal classé : %+v", ctx)
	}
	if ctx.Short != "-c" {
		t.Fatalf("ctx-size short=%q", ctx.Short)
	}
	fa := byID["flash-attn"]
	if fa.Kind != "enum" || !ContainsFold(fa.Choices, "auto") || fa.Default != "auto" {
		t.Fatalf("flash-attn : %+v", fa)
	}
	if fa.Tier != "advanced" {
		t.Fatalf("flash-attn tier=%s", fa.Tier)
	}
	lm := byID["load-mode"]
	if !ContainsFold(lm.Choices, "mmap") || !ContainsFold(lm.Choices, "mlock") {
		t.Fatalf("load-mode choices=%v", lm.Choices)
	}
	if byID["mlock"].Deprecated != true {
		t.Fatal("mlock devrait être deprecated")
	}
	if !byID["help"].Hidden || !byID["model"].Hidden || !byID["host"].Hidden || !byID["port"].Hidden {
		t.Fatalf("flags machine encore visibles : help=%v model=%v host=%v port=%v", byID["help"].Hidden, byID["model"].Hidden, byID["host"].Hidden, byID["port"].Hidden)
	}
	if byID["swa-full"].Kind != "bool" || byID["swa-full"].Tier != "expert" {
		t.Fatalf("swa-full : %+v", byID["swa-full"])
	}
	if byID["temp"].Key != "TEMP" {
		t.Fatalf("temp key=%q aliases=%v", byID["temp"].Key, byID["temp"].Aliases)
	}
	if byID["reasoning"].Key != "REASONING" || byID["reasoning"].Tier != "base" {
		t.Fatalf("reasoning : %+v", byID["reasoning"])
	}
	if byID["mmproj-auto"].Kind != "bool" {
		t.Fatalf("mmproj-auto kind=%s aliases=%v", byID["mmproj-auto"].Kind, byID["mmproj-auto"].Aliases)
	}
	st := byID["spec-type"]
	if st.Kind != "enum" || !ContainsFold(st.Choices, "draft-mtp") || st.Default != "none" || st.Tier != "advanced" {
		t.Fatalf("spec-type : %+v", st)
	}
	md := byID["model-draft"]
	if md.Key != "MODEL_DRAFT" {
		t.Fatalf("model-draft : %+v", md)
	}
}

func TestChatTemplateThinks(t *testing.T) {
	if !ChatTemplateThinks("{% if enable_thinking %}<think>{{ thinking }}</think>{% endif %}") {
		t.Fatal("qwen think template")
	}
	if ChatTemplateThinks("{{ message.content }}") {
		t.Fatal("plain instruct ne doit pas penser")
	}
}

func TestInspectChatTemplateEffortFromGemmaAndQwen(t *testing.T) {
	gemma := `{%- if (enable_thinking is defined and enable_thinking) or tools -%}
{%- if not enable_thinking | default(false) -%}
{%- macro strip_thinking(text) -%}{% endmacro %}`
	g := InspectChatTemplate(gemma)
	if !g.Thinks {
		t.Fatal("Gemma 4 : enable_thinking → raisonnement")
	}
	if g.HasEffort || len(g.EffortLevels) > 0 {
		t.Fatalf("Gemma 4 n'a pas de niveaux d'effort : %+v", g)
	}

	plain := InspectChatTemplate("{{ message.content }}")
	if plain.Thinks || plain.HasEffort {
		t.Fatalf("instruct nu : %+v", plain)
	}

	qwen := `{%- set resolved_reasoning_effort = reasoning_effort|default('xhigh') %}
    {%- if resolved_reasoning_effort not in ('xhigh', 'medium', 'low') %}
       {{- raise_exception('Unexpected reasoning effort ' ~ reasoning_effort ~ '. Supported types are xhigh (default), medium, and low.') }}
    {%- endif %}
    {%- if enable_thinking %}<think>{{ thinking }}</think>{% endif %}`
	q := InspectChatTemplate(qwen)
	if !q.Thinks || !q.HasEffort {
		t.Fatalf("Qwen doit penser avec effort : %+v", q)
	}
	if q.EffortDefault != "xhigh" {
		t.Fatalf("défaut Qwen = %q", q.EffortDefault)
	}
	want := []string{"low", "medium", "xhigh"}
	if len(q.EffortLevels) != len(want) {
		t.Fatalf("niveaux Qwen = %v", q.EffortLevels)
	}
	for i, w := range want {
		if q.EffortLevels[i] != w {
			t.Fatalf("niveaux Qwen = %v", q.EffortLevels)
		}
	}
}

func keysOf(m map[string]LlamaFlag) []string {
	var k []string
	for id := range m {
		k = append(k, id)
	}
	return k
}
