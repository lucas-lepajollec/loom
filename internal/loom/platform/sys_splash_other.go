//go:build !windows

package platform

// sys_splash_other.go — pas de fenêtre de démarrage hors Windows (no-op).

type Splash struct{}

func ShowSplash(text string) *Splash { return &Splash{} }
func (s *Splash) Close()             {}
