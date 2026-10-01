package loom

// Provider records belong to Loom; the cloud protocol lives in runtime/openai.
type CloudProvider struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Endpoint  string   `json:"endpoint"`
	Model     string   `json:"model"`
	Models    []string `json:"models,omitempty"`
	UsageMode string   `json:"usage_mode,omitempty"` // "none" for APIs rejecting stream_options.
	// Remember: the key is kept in the OS keychain (never in Loom files).
	Remember bool `json:"remember,omitempty"`
	// Credentials are intentionally absent from this persistent record.
	Ready bool `json:"ready"`
}
