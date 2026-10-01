package loom

import (
	"os"
	"os/signal"
	"syscall"
)

func (m *runtimeSessions) installACPShutdown() {
	m.shutdownOnce.Do(func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		go func() {
			sig := <-signals
			m.shutdownACP()
			signal.Stop(signals)
			if sig == os.Interrupt {
				os.Exit(130)
			}
			os.Exit(143)
		}()
	})
}
