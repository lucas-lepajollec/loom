package loom

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type llamaHelpCache struct {
	bin   string
	mod   int64
	text  string
	flags []LlamaFlag
}

var (
	llamaHelpMu    sync.Mutex
	llamaHelpState llamaHelpCache
)

func llamaHelpText(bin string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(LoomHome(), bin)
	}
	bin = prebuiltResolveBin(bin)
	mod := int64(0)
	if st, err := os.Stat(bin); err == nil {
		mod = st.ModTime().UnixNano()
	}
	llamaHelpMu.Lock()
	if llamaHelpState.bin == bin && llamaHelpState.mod == mod && llamaHelpState.text != "" {
		t := llamaHelpState.text
		llamaHelpMu.Unlock()
		return t
	}
	llamaHelpMu.Unlock()

	setLibraryPath(filepath.Dir(bin))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := hideCmd(exec.CommandContext(ctx, bin, "--help"))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	text := out.String()

	llamaHelpMu.Lock()
	llamaHelpState = llamaHelpCache{bin: bin, mod: mod, text: text, flags: parseLlamaHelp(text)}
	llamaHelpMu.Unlock()
	return text
}

func llamaFlagsForBin(bin string) []LlamaFlag {
	_ = llamaHelpText(bin)
	llamaHelpMu.Lock()
	defer llamaHelpMu.Unlock()
	if llamaHelpState.bin == "" {
		return nil
	}
	out := make([]LlamaFlag, len(llamaHelpState.flags))
	copy(out, llamaHelpState.flags)
	return out
}

func llamaHasFlag(bin, id string) bool {
	id = strings.TrimPrefix(strings.TrimSpace(id), "--")
	for _, f := range llamaFlagsForBin(bin) {
		if f.ID == id {
			return true
		}
		for _, a := range f.Aliases {
			if strings.TrimPrefix(a, "--") == id || strings.TrimPrefix(a, "-") == id {
				return true
			}
		}
	}
	return false
}

func llamaBooleanFlag(bin, id string) bool {
	id = strings.TrimPrefix(strings.TrimSpace(id), "--")
	for _, f := range llamaFlagsForBin(bin) {
		if f.ID == id {
			return f.Kind == "bool"
		}
	}
	return false
}

func llamaFlagsPublic(bin string) []LlamaFlag {
	var out []LlamaFlag
	for _, f := range llamaFlagsForBin(bin) {
		if f.Hidden || f.ID == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}
