//go:build darwin && !cgo

package loom

// A cross-compiled, CGO-free macOS binary keeps the CLI/web server available.
// Official native macOS releases still build the Cocoa menu-bar integration.
func runTray(url string) { select {} }
