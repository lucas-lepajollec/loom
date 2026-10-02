package resources

import "testing"

func TestAdoptedMCPNeverCopiesSecretsAndStartsDisabled(t *testing.T) {
	cfg, err := AdoptHarnessMCP("node", "", []string{"s.js"}, []string{"GITHUB_TOKEN", "PATH"})
	if err != nil || cfg.Enabled || cfg.Env["GITHUB_TOKEN"] != "" || len(cfg.Env) != 1 || cfg.Command != "node" {
		t.Fatalf("%+v %v", cfg, err)
	}
	if _, err := AdoptHarnessMCP("", "", nil, nil); err == nil {
		t.Fatal("incomplete definition adopted")
	}
	if c, _ := AdoptHarnessMCP("", "https://m", nil, nil); c.URL != "https://m" {
		t.Fatal("http server")
	}
}
