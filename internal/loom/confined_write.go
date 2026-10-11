package loom

import (
	"bytes"
	"crypto/rand"

	"os"
	"path/filepath"
)

func gatewayRootWrite(root *os.Root, rel string, data []byte) error {
	if current, err := root.ReadFile(rel); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := root.MkdirAll(filepath.Dir(rel), 0700); err != nil {
		return err
	}
	tmpName := filepath.Join(filepath.Dir(rel), ".loom-gateway-"+rand.Text())
	f, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmpName)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(tmpName, rel)
	}
	return err
}
