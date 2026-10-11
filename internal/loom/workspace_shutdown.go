package loom

import (
	"os"
	"os/signal"
	"syscall"
)

func (m *runtimeSessions) installRuntimeShutdown() {
	m.shutdownOnce.Do(func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		go func() {
			sig := <-signals
			m.shutdownRuntimes()
			shutdownTerminals()
			vllm.stop()
			// A llama.cpp started by this process (no system service) would
			// otherwise outlive it, unknown to the next Loom, holding VRAM.
			if ownedLlamaManaged() {
				stopOwnedLlama()
			}
			signal.Stop(signals)
			if sig == os.Interrupt {
				os.Exit(130)
			}
			os.Exit(143)
		}()
	})
}
