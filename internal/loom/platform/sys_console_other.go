//go:build !windows && !darwin

package platform

// setupConsole : sous Linux, le process a toujours une vraie console/tty
// standard (stdout hérité). Rien à faire → on signale simplement « on a une
// console ». Les cas « lancé par un clic » sont traités par
// sys_console_windows.go (double-clic sur loom.exe) et sys_console_darwin.go
// (ouverture de Loom.app depuis le Finder).
func SetupConsole() bool { return true }

// appWarning : rien à signaler hors macOS (l'App Translocation est spécifique à
// Gatekeeper). Voir sys_console_darwin.go.
func AppWarning() string { return "" }
