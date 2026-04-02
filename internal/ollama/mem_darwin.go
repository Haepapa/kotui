//go:build darwin

package ollama

import "golang.org/x/sys/unix"

// totalSystemMemory returns the total physical RAM on macOS (including Apple Silicon
// unified memory, which serves as VRAM).
func totalSystemMemory() (uint64, error) {
	b, err := unix.SysctlRaw("hw.memsize")
	if err != nil {
		return 0, err
	}
	if len(b) < 8 {
		return 0, nil
	}
	// hw.memsize is a little-endian uint64 on arm64/amd64.
	var mem uint64
	for i := 0; i < 8; i++ {
		mem |= uint64(b[i]) << (8 * i)
	}
	return mem, nil
}
