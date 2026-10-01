package loom

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	return llamaOwner.HelpText(bin, func() string {
		setLibraryPath(filepath.Dir(bin))
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		cmd := hideCmd(exec.CommandContext(ctx, bin, "--help"))
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		_ = cmd.Run()
		return out.String()
	})
}

func llamaFlagsForBin(bin string) []LlamaFlag {
	_ = llamaHelpText(bin)
	return llamaOwner.HelpFlags()
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
