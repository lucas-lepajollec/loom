//go:build windows

package loom

import "errors"

// Windows needs ConPTY; terminals come later there.
const ptySupported = false

func startPTY(argv []string, dir string, env []string) (termProcess, error) {
	return nil, errors.New("les terminaux ne sont pas encore disponibles sous Windows")
}
