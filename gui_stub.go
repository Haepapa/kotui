//go:build headless

// gui_stub.go compiles only when the "headless" build tag IS set.
// It provides a no-op runGUI so the headless binary compiles without Wails.
package main

import (
	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/store"
)

// runGUI is unreachable in headless mode — main() always takes the headless
// branch when the binary is built with -tags headless.
func runGUI(_ config.Config, _ *store.DB) {
	panic("kotui: GUI not available in headless build (compiled with -tags headless)")
}
