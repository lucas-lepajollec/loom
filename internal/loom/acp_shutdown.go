package loom

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var acpShutdownOnce sync.Once

func installACPShutdown() {
	acpShutdownOnce.Do(func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		go func() {
			sig := <-signals
			workspaceSessions.shutdownACP()
			signal.Stop(signals)
			if sig == os.Interrupt {
				os.Exit(130)
			}
			os.Exit(143)
		}()
	})
}
