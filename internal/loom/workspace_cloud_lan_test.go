package loom

import "testing"

func TestCloudEndpointAllowsHTTPOnLocalNetworkOnly(t *testing.T) {
	ok := []string{"http://127.0.0.1:11434/v1", "http://localhost:1234/v1", "http://192.168.1.20:11434/v1", "http://10.0.0.5:8000/v1",
		"http://100.101.1.2:11434/v1", "http://nas:11434/v1", "http://ollama.lan/v1", "http://box.local:8000/v1", "http://[::1]:8080/v1", "https://api.openai.com/v1"}
	for _, u := range ok {
		if _, err := validateCloudEndpoint(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	bad := []string{"http://api.openai.com/v1", "http://8.8.8.8/v1", "http://example.com:11434/v1", "ftp://127.0.0.1/v1"}
	for _, u := range bad {
		if _, err := validateCloudEndpoint(u); err == nil {
			t.Errorf("%s accepté", u)
		}
	}
}
