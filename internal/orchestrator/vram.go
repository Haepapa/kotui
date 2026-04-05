package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/pkg/models"
)

// vramCoolingPeriod is the delay after parking the Lead model before a Worker
// is loaded. Gives the GPU driver time to release memory.
const vramCoolingPeriod = 500 * time.Millisecond

// VRAMCoordinator manages model loading and unloading according to the
// system's VRAM profile. In dual mode both Lead and Worker can coexist.
// In swap mode the Lead must be parked before any Worker is loaded.
type VRAMCoordinator struct {
	profile   models.VRAMProfile
	inferrer  Inferrer
	leadModel string

	mu                 sync.Mutex
	parked             bool        // true when Lead has been parked (swap mode only)
	currentWorkerModel string      // model used by the most recent worker slot
	workerHot          bool        // worker model still in VRAM (lead hasn't reloaded)
	preWarmModel       string      // model currently being pre-warmed (dual mode only)
	queue              chan struct{} // allows only 1 worker slot in swap mode
}

// newVRAMCoordinator creates a coordinator for the given hardware profile.
func newVRAMCoordinator(profile models.VRAMProfile, inferrer Inferrer, leadModel string) *VRAMCoordinator {
	q := make(chan struct{}, 1)
	q <- struct{}{} // start with one slot available
	return &VRAMCoordinator{
		profile:   profile,
		inferrer:  inferrer,
		leadModel: leadModel,
		queue:     q,
	}
}

// AcquireWorkerSlot blocks until a Worker slot is available.
// In dual mode this returns immediately (unbuffered slot).
// In swap mode this serialises workers and parks the Lead.
//
// model is the model the worker will use. In swap mode, if model matches the
// model from the previous slot and the worker is still hot in VRAM (logical
// swap), the park+cooling cycle is skipped.
func (v *VRAMCoordinator) AcquireWorkerSlot(ctx context.Context, model string) error {
	select {
	case <-v.queue:
	case <-ctx.Done():
		return ctx.Err()
	}

	if v.profile == models.VRAMSwap {
		v.mu.Lock()
		sameModel := model != "" && model == v.currentWorkerModel && v.workerHot
		v.mu.Unlock()

		if !sameModel {
			if err := v.parkLead(ctx); err != nil {
				v.queue <- struct{}{} // release slot on failure
				return fmt.Errorf("vram: park lead: %w", err)
			}
			// Cooling period: give the GPU driver time to release memory before
			// the next model is loaded.
			time.Sleep(vramCoolingPeriod)
		}
		// else: logical swap — same model still in VRAM, skip park+cooling.
	}

	v.mu.Lock()
	v.currentWorkerModel = model
	v.workerHot = false
	v.mu.Unlock()

	return nil
}

// ReleaseWorkerSlot signals that the Worker has finished.
// In swap mode, the Lead will reload on its next inference call.
func (v *VRAMCoordinator) ReleaseWorkerSlot(ctx context.Context) {
	if v.profile == models.VRAMSwap {
		v.mu.Lock()
		v.parked = false
		v.workerHot = true // worker model still in VRAM; lead hasn't reloaded yet
		v.mu.Unlock()
		// The Lead reloads naturally on next Chat() call — no explicit wake needed.
	}
	v.queue <- struct{}{} // return the slot
}

// NotifyLeadRunning marks the worker model as no longer hot.
// Call this before any lead TurnStream so that subsequent worker acquisitions
// know the lead model has been (or is about to be) loaded, invalidating the
// logical-swap optimisation.
func (v *VRAMCoordinator) NotifyLeadRunning() {
	v.mu.Lock()
	v.workerHot = false
	v.mu.Unlock()
}

// PreWarm pre-loads a worker model in the background (dual mode only).
// In VRAMDual profile both models can coexist, so warming the worker while
// the Lead is still responding eliminates cold-start latency for the first
// specialist task. Redundant calls for the same model are no-ops.
func (v *VRAMCoordinator) PreWarm(ctx context.Context, model string) {
	if v.profile != models.VRAMDual || model == "" {
		return
	}
	v.mu.Lock()
	if v.preWarmModel == model {
		v.mu.Unlock()
		return
	}
	v.preWarmModel = model
	v.mu.Unlock()

	go func() {
		_, _ = v.inferrer.Chat(ctx, ollama.ChatRequest{
			Model:     model,
			Messages:  []ollama.ChatMessage{{Role: "user", Content: ""}},
			KeepAlive: ollama.Forever(),
		})
	}()
}

// parkLead sends a minimal keep_alive=0 chat to force the Lead model to
// unload from VRAM. This is a best-effort operation; if it fails we log
// but continue — the Worker may hit an OOM on very constrained hardware.
func (v *VRAMCoordinator) parkLead(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.parked {
		return nil
	}
	_, err := v.inferrer.Chat(ctx, ollama.ChatRequest{
		Model:     v.leadModel,
		Messages:  []ollama.ChatMessage{{Role: "user", Content: ""}},
		KeepAlive: ollama.Release(),
	})
	if err != nil {
		// Not fatal — the model may have already unloaded or Ollama may return
		// an error for an empty message. Mark as parked and continue.
	}
	v.parked = true
	return nil
}

// Profile returns the detected hardware profile.
func (v *VRAMCoordinator) Profile() models.VRAMProfile { return v.profile }

// IsParked reports whether the Lead is currently parked (swap mode only).
func (v *VRAMCoordinator) IsParked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.parked
}
