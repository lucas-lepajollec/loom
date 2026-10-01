package platform

import "testing"

func TestEngineCmdlineOwned(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/opt/llama.cpp/build/bin/llama-server\x00--port\x008081", true},
		{"/tmp/go-build/exe/loom\x00serve", true},
		{"/usr/local/bin/loom\x00serve", true},
		{"/usr/bin/firefox", false},
		{"/usr/local/bin/loom-engine", false},
		{"", false},
	}
	for _, c := range cases {
		if got := EngineCmdlineOwned(c.in); got != c.want {
			t.Errorf("engineCmdlineOwned(%q)=%v want %v", c.in, got, c.want)
		}
	}
}
