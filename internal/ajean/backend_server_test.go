package ajean

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseLlamaSlots(t *testing.T) {
	raw := []byte(`[
	  {"id":0,"id_task":12,"n_ctx":8192,"is_processing":true,"next_token":{"n_decoded":40,"n_remain":-1}},
	  {"id":1,"id_task":0,"n_ctx":8192,"is_processing":false,"next_token":{"n_decoded":0,"n_remain":-1}}
	]`)
	got := parseLlamaSlots(raw)
	if len(got) != 2 {
		t.Fatalf("slots = %d, attendu 2", len(got))
	}
	if !got[0].IsProcessing || got[0].NDecoded != 40 || got[1].IsProcessing {
		t.Fatalf("parse incorrect : %+v", got)
	}
	if parseLlamaSlots([]byte(`{"error":"no"}`)) != nil {
		t.Fatal("un objet d'erreur ne doit pas passer pour une liste de slots")
	}
}

func TestParseLlamaSlotsArrayNextToken(t *testing.T) {
	raw := []byte(`[
	  {"id":0,"id_task":15,"n_ctx":8192,"is_processing":true,"next_token":[{"n_decoded":867,"n_remain":880,"has_next_token":true}]},
	  {"id":1,"id_task":0,"n_ctx":8192,"is_processing":false,"next_token":[{"n_decoded":0,"n_remain":0,"has_next_token":false}]}
	]`)
	got := parseLlamaSlots(raw)
	if len(got) != 2 {
		t.Fatalf("slots = %d, attendu 2", len(got))
	}
	if !got[0].IsProcessing || got[0].NDecoded != 867 || got[0].NRemain != 880 {
		t.Fatalf("parse tableau next_token incorrect : %+v", got[0])
	}
}

func TestParseLlamaSlotsTimings(t *testing.T) {
	raw := []byte(`[
	  {
	    "id":0,
	    "id_task":3,
	    "n_ctx":4096,
	    "is_processing":true,
	    "n_decoded":88,
	    "n_prompt_tokens":412,
	    "timings":{"prompt_n":412,"predicted_n":88,"predicted_per_second":50.4}
	  }
	]`)
	got := parseLlamaSlots(raw)
	if len(got) != 1 {
		t.Fatalf("slots = %d", len(got))
	}
	s := got[0]
	if s.NDecoded != 88 || s.PromptN != 412 || s.PredTokS < 50 || s.PredTokS > 51 {
		t.Fatalf("timings = %+v", s)
	}
}

func TestParseLlamaSlotsWrapped(t *testing.T) {
	raw := []byte(`{"slots":[{"id":2,"is_processing":true,"n_decoded":5}]}`)
	got := parseLlamaSlots(raw)
	if len(got) != 1 || got[0].ID != 2 || got[0].NDecoded != 5 {
		t.Fatalf("wrap = %+v", got)
	}
}

func TestNoteServerSlotsRecordsCompletion(t *testing.T) {
	resetServerWatch()
	busy := []llamaSlot{{ID: 0, IDTask: 7, IsProcessing: true, NDecoded: 12, PromptN: 40}}
	noteServerSlots(busy)
	idle := []llamaSlot{{ID: 0, IDTask: 7, IsProcessing: false, NDecoded: 0}}
	hist, _ := noteServerSlots(idle)
	if len(hist) != 1 || hist[0].Task != 7 || hist[0].Tokens != 12 || hist[0].Prompt != 40 {
		t.Fatalf("historique = %+v, attendu une requête terminée", hist)
	}
	st := serverStats(0, 0)
	if st["completed"] != 1 || st["tokens"] != 12 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestNoteServerSlotsLiveTokS(t *testing.T) {
	resetServerWatch()
	noteServerSlots([]llamaSlot{{ID: 0, IDTask: 1, IsProcessing: true, NDecoded: 10, PredTokS: 42.2}})
	_, items := noteServerSlots([]llamaSlot{{ID: 0, IDTask: 1, IsProcessing: true, NDecoded: 20, PredTokS: 42.2}})
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	toks, _ := items[0]["toks"].(float64)
	if toks < 42 || toks > 43 {
		t.Fatalf("toks live = %v, attendu timings llama-server", toks)
	}
	if items[0]["tokens"] != 20 || items[0]["busy"] != true {
		t.Fatalf("item = %+v", items[0])
	}
}

func TestSetServerNPBounds(t *testing.T) {
	testHome(t)
	if err := setServerNP(0); err != nil {
		t.Fatal(err)
	}
	if serverNP() != 1 {
		t.Fatalf("np=0 doit devenir 1, got %d", serverNP())
	}
	if err := setServerNP(99); err != nil {
		t.Fatal(err)
	}
	if serverNP() != 32 {
		t.Fatalf("np trop haut = %d, attendu 32", serverNP())
	}
}

func TestParseLlamaSlotsJSONRoundTrip(t *testing.T) {
	s := []map[string]any{{"id": 2, "id_task": 9, "is_processing": true, "n_ctx": 4096, "n_decoded": 3}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := parseLlamaSlots(b)
	if len(got) != 1 || got[0].ID != 2 || got[0].NCtx != 4096 || got[0].NDecoded != 3 {
		t.Fatalf("roundtrip = %+v", got)
	}
}

func TestAPIKeyRequiredGenerates(t *testing.T) {
	testHome(t)
	if apiKeyRequired() || readAPIKey() != "" {
		t.Fatal("départ : pas de clé")
	}
	if err := setAPIKeyRequired(true); err != nil {
		t.Fatal(err)
	}
	if !apiKeyRequired() || readAPIKey() == "" {
		t.Fatal("exiger une clé doit en générer une")
	}
	k := readAPIKey()
	if err := setAPIKeyRequired(false); err != nil {
		t.Fatal(err)
	}
	if apiKeyRequired() {
		t.Fatal("exigence retirée")
	}
	if readAPIKey() != k {
		t.Fatal("retirer l'exigence ne doit pas effacer la clé")
	}
}

func TestNoteServerSlotsElapsed(t *testing.T) {
	resetServerWatch()
	noteServerSlots([]llamaSlot{{ID: 0, IDTask: 4, IsProcessing: true, NDecoded: 1}})
	time.Sleep(20 * time.Millisecond)
	_, items := noteServerSlots([]llamaSlot{{ID: 0, IDTask: 4, IsProcessing: true, NDecoded: 8}})
	el, _ := items[0]["elapsed_ms"].(int64)
	if el < 10 {
		t.Fatalf("elapsed_ms = %d, attendu un suivi du temps", el)
	}
}
