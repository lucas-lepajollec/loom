package loom

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBrowserAccountURLBoundary(t *testing.T) {
	claude := "https://claude.ai/oauth/authorize?state=fixture&client_id=fixture"
	google := "https://accounts.google.com/o/oauth2/auth?state=fixture&client_id=fixture"
	for _, tc := range []struct{ text, runtime, want string }{
		{"\x1b]8;;" + claude + "\x07Sign in\x1b]8;;\x07", "claude-code", claude},
		{google, "antigravity", google}, {google, "claude-code", ""}, {claude, "antigravity", ""},
		{"https://claude.ai.attacker.example/oauth/authorize?state=fixture&client_id=fixture", "claude-code", ""},
		{"https://claude.ai@attacker.example/oauth/authorize?state=fixture&client_id=fixture", "claude-code", ""},
		{"https://claude.ai:1234/oauth/authorize?state=fixture&client_id=fixture", "claude-code", ""},
		{"https://claude.ai/oauth/authorize", "claude-code", ""},
		{claude + "#fragment", "claude-code", ""},
	} {
		if got := accountAuthorizationURL(tc.text, tc.runtime); got != tc.want {
			t.Errorf("URL boundary: %q", got)
		}
	}
	for _, code := range []string{"\n/logout", "/logout\n", "TEST CODE", "", "ab", strings.Repeat("a", 1001)} {
		if nativeAuthorizationCode.MatchString(code) {
			t.Fatal("invalid code accepted")
		}
	}
}

func TestBrowserAccountHelper(t *testing.T) {
	mode := os.Getenv("LOOM_TEST_BROWSER_ACCOUNT")
	if mode == "" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	provider := "claude.ai/oauth/authorize"
	if mode == "google" {
		fmt.Println("Select login method:\n > 1. Google OAuth")
		if line, _ := r.ReadString('\n'); strings.TrimSpace(line) != "" {
			os.Exit(30)
		}
		provider = "accounts.google.com/o/oauth2/auth"
	}
	fmt.Printf("\x1b]8;;https://%s?state=fixture&client_id=fixture\x07Sign in\x1b]8;;\x07\nPaste authorization code below:\n", provider)
	if mode == "wait" {
		time.Sleep(time.Minute)
		os.Exit(31)
	}
	if line, _ := r.ReadString('\n'); strings.TrimSpace(line) != "TEST-CODE" {
		os.Exit(32)
	}
	if mode == "google" {
		fmt.Println("Authentication successful!")
	} else {
		fmt.Println("Login successful")
	}
	time.Sleep(time.Minute) // Owner must close and reap us after completion.
	os.Exit(0)
}

func TestBrowserAccountPrivatePTYAndCancellation(t *testing.T) {
	if !ptySupported {
		t.Skip("native pseudo-terminal unavailable")
	}
	for _, mode := range []string{"claude", "google", "wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.Command(os.Args[0], "-test.run=^TestBrowserAccountHelper$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "LOOM_TEST_BROWSER_ACCOUNT="+mode)
			runtime := "claude-code"
			if mode == "google" {
				runtime = "antigravity"
			}
			job := &harnessAccountJob{harnessAccountState: harnessAccountState{State: "starting"}, codes: make(chan string, 1)}
			done := make(chan error, 1)
			go func() { done <- runBrowserAccount(ctx, cmd, runtime, job) }()
			for !job.snapshot().Input && ctx.Err() == nil {
				time.Sleep(5 * time.Millisecond)
			}
			if !job.snapshot().Input {
				t.Fatal("native browser/code prompt not exposed")
			}
			if mode == "wait" {
				cancel()
			} else {
				job.codes <- "TEST-CODE"
			}
			select {
			case err := <-done:
				if (mode != "wait") != (err == nil) {
					t.Fatalf("%s: %v", mode, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("owned login process did not stop")
			}
		})
	}
}
