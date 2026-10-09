package loom

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Files attached to a cloud or agent discussion. A cloud model has no file
// tools, so text files travel inside the message; an agent reads them itself
// from Loom's upload folder. Preview and send build the same text, so the
// prepared context revision stays identical.
func composeTurnText(s RuntimeSession, text string, names []string) (string, error) {
	if len(names) == 0 {
		return text, nil
	}
	files := attachFiles(names)
	if len(files) == 0 {
		return "", errors.New("attached files are no longer available; attach them again")
	}
	dir, err := uploadsDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		text = "Read the attached file."
		if len(files) > 1 {
			text = "Read the attached files."
		}
	}
	var b strings.Builder
	b.WriteString(text)
	if s.RuntimeID != "openai-compatible" && s.RuntimeID != "llama.cpp" {
		if a, ok := acpAgentFor(s.RuntimeID); ok && a.Remote {
			return "", errors.New("attaching files to an agent on another machine is not available yet")
		}
		b.WriteString("\n\nAttached files (read them from these paths):")
		for _, f := range files {
			fmt.Fprintf(&b, "\n<loom-file name=%q path=%q size=\"%d\" />", f.Name, filepath.Join(dir, f.Name), f.Size)
		}
		return b.String(), nil
	}
	budget := maxMessageBytes - len(text) - 512
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil {
			return "", err
		}
		if !utf8.Valid(data) || imageMime(f.Name) != "" {
			fmt.Fprintf(&b, "\n\n<loom-file name=%q size=\"%d\" omitted=\"binary\" />", f.Name, f.Size)
			continue
		}
		note := ""
		if room := budget - len(f.Name) - 64; len(data) > room {
			if room < 0 {
				room = 0
			}
			for room > 0 && !utf8.RuneStart(data[room]) {
				room--
			}
			note = fmt.Sprintf(" truncated=\"%d of %d bytes\"", room, len(data))
			data = data[:room]
		}
		fmt.Fprintf(&b, "\n\n<loom-file name=%q%s>\n%s\n</loom-file>", f.Name, note, data)
		budget -= len(data) + len(f.Name) + 64
	}
	return b.String(), nil
}
