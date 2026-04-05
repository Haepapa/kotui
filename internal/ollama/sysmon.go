package ollama

import (
	"context"
	"sync/atomic"
	"time"

	gcpu "github.com/shirou/gopsutil/v3/cpu"
	gmem "github.com/shirou/gopsutil/v3/mem"
)

const (
	cpuPressureThreshold = 90.0 // percent usage
	ramFreeMinPct        = 10.0 // minimum free RAM percent before pressure
	sysMonPollInterval   = 2 * time.Second
)

// SystemMonitor polls CPU and RAM usage every 2 seconds.
// Call Start to begin monitoring in the background.
// IsUnderPressure returns true when CPU > 90% OR free RAM < 10%.
type SystemMonitor struct {
	pressure atomic.Bool
}

// NewSystemMonitor returns a stopped SystemMonitor. Call Start to begin.
func NewSystemMonitor() *SystemMonitor {
	return &SystemMonitor{}
}

// Start launches the background polling goroutine. It runs until ctx is cancelled.
func (m *SystemMonitor) Start(ctx context.Context) {
	go m.run(ctx)
}

// IsUnderPressure returns true when the latest sample detected high resource usage.
// Safe for concurrent use; reads an atomic bool.
func (m *SystemMonitor) IsUnderPressure() bool {
	return m.pressure.Load()
}

func (m *SystemMonitor) run(ctx context.Context) {
	// Warm up CPU sampler — first gopsutil call establishes the baseline interval.
	_, _ = gcpu.Percent(0, false)

	ticker := time.NewTicker(sysMonPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pressure.Store(m.sample())
		}
	}
}

func (m *SystemMonitor) sample() bool {
	// CPU: non-blocking — uses delta since the last call (warmed up in run).
	cpuPcts, err := gcpu.Percent(0, false)
	if err == nil && len(cpuPcts) > 0 && cpuPcts[0] >= cpuPressureThreshold {
		return true
	}

	// RAM: check if available memory is below the minimum free threshold.
	vmStat, err := gmem.VirtualMemory()
	if err == nil && vmStat.Total > 0 {
		freePercent := float64(vmStat.Available) / float64(vmStat.Total) * 100
		if freePercent < ramFreeMinPct {
			return true
		}
	}

	return false
}
