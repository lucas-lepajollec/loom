package loom

import (
	"sync"
)

// Keep the actual tail bounded while the process writes, including stderr.
type commandTail struct {
	mu   sync.Mutex
	data []byte
}

func (b *commandTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= commandLogLimit {
		b.data = append(b.data[:0], p[n-commandLogLimit:]...)
	} else {
		if extra := len(b.data) + n - commandLogLimit; extra > 0 {
			b.data = append(b.data[:0], b.data[extra:]...)
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *commandTail) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }

const commandLogLimit = 32 * 1024
