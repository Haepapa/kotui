//go:build !darwin && !linux

package ollama

// totalSystemMemory returns 0 on unsupported platforms, causing DetectVRAMProfile
// to conservatively return VRAMSwap.
func totalSystemMemory() (uint64, error) {
	return 0, nil
}
