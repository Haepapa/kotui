// Package warroom is the Wails-registered service layer for the War Room UI.
//
// It bridges the backend (Orchestrator, Dispatcher, Store) to the Svelte
// frontend by exposing RPC-style methods (callable via Call.ByName from
// TypeScript) and emitting real-time Wails events whenever the Dispatcher
// publishes a message.
//
// Event names emitted:
//
//	kotui:message    — models.Message (agent/system messages)
//	kotui:heartbeat  — HeartbeatState (bottom-bar status)
//	kotui:error      — { "error": "..." } (background task failures)
//	kotui:agents     — []AgentInfo (agent roster update)
package warroom

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/orchestrator"
	"github.com/haepapa/kotui/internal/store"
	"github.com/haepapa/kotui/pkg/models"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// AgentInfo summarises a known agent's current state for the UI.
type AgentInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"` // idle | working | parked | offline
	Model  string `json:"model"`
}

// HeartbeatState captures the current system health for the bottom bar.
type HeartbeatState struct {
	IsHealthy   bool      `json:"is_healthy"`
	Phase       string    `json:"phase"`
	Breadcrumbs []string  `json:"breadcrumbs"`
	ActiveCount int       `json:"active_count"`
	VRAMProfile string    `json:"vram_profile"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WarRoomService is the Wails-registered service binding.
// All exported methods become callable from TypeScript via Call.ByName.
type WarRoomService struct {
	app  *application.App
	db   *store.DB
	orch *orchestrator.Orchestrator
	disp *dispatcher.Dispatcher

	mu           sync.RWMutex
	activeConvID string
	heartbeat    HeartbeatState
	unsub        func()
}

// New creates a WarRoomService and starts the Dispatcher event bridge.
func New(
	app *application.App,
	db *store.DB,
	orch *orchestrator.Orchestrator,
	disp *dispatcher.Dispatcher,
) *WarRoomService {
	s := &WarRoomService{
		app:  app,
		db:   db,
		orch: orch,
		disp: disp,
		heartbeat: HeartbeatState{
			IsHealthy:   true,
			Phase:       "Idle",
			Breadcrumbs: []string{"Idle"},
			UpdatedAt:   time.Now(),
		},
	}
	s.startEventBridge()
	return s
}

// startEventBridge subscribes to all Dispatcher messages and re-emits them
// as Wails events so the frontend receives live updates.
func (s *WarRoomService) startEventBridge() {
	s.unsub = s.disp.Subscribe("", func(msg models.Message) {
		// Evolve heartbeat breadcrumbs based on message kind.
		s.mu.Lock()
		hb := &s.heartbeat
		switch msg.Kind {
		case models.KindBossCommand:
			hb.Phase = "Planning"
			hb.Breadcrumbs = []string{"Planning"}
		case models.KindAgentMessage:
			if hb.Phase == "Planning" {
				hb.Phase = "Working"
				hb.Breadcrumbs = append(hb.Breadcrumbs, "Working")
			}
		case models.KindToolCall:
			hb.Phase = "Executing"
			if len(hb.Breadcrumbs) == 0 || hb.Breadcrumbs[len(hb.Breadcrumbs)-1] != "Executing" {
				hb.Breadcrumbs = append(hb.Breadcrumbs, "Executing")
			}
		case models.KindMilestone:
			hb.Phase = "Idle"
			hb.Breadcrumbs = append(hb.Breadcrumbs, "✓ Done")
		case models.KindSystemEvent:
			// no breadcrumb change
		}
		hb.UpdatedAt = time.Now()
		snapshot := *hb
		s.mu.Unlock()

		s.app.Event.Emit("kotui:message", msg)
		s.app.Event.Emit("kotui:heartbeat", snapshot)
	})
}

// Shutdown stops the event bridge. Called on app teardown.
func (s *WarRoomService) Shutdown() {
	if s.unsub != nil {
		s.unsub()
	}
}

// --- Exported methods callable from TypeScript ---------------------------

// GetProjects returns all projects ordered by creation date (newest first).
func (s *WarRoomService) GetProjects(ctx context.Context) ([]models.Project, error) {
	if s.db == nil {
		return nil, nil
	}
	return s.db.ListProjects(ctx)
}

// CreateProject creates a new project, marks it active, and switches to it.
func (s *WarRoomService) CreateProject(ctx context.Context, name, description string) (*models.Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project name is required")
	}
	p := models.Project{
		ID:          store.NewID(),
		Name:        name,
		Description: description,
		Active:      true,
		CreatedAt:   time.Now(),
	}
	if err := s.db.CreateProject(ctx, p); err != nil {
		return nil, err
	}
	if err := s.SwitchProject(ctx, p.ID); err != nil {
		return nil, err
	}
	return &p, nil
}

// SwitchProject marks a project as active and resets the Orchestrator context.
func (s *WarRoomService) SwitchProject(ctx context.Context, id string) error {
	if err := s.db.SetActiveProject(ctx, id); err != nil {
		return err
	}
	if s.orch != nil {
		if err := s.orch.SetProject(ctx, id); err != nil {
			return err
		}
	}
	// Retrieve the conversation that SetProject just created.
	convID, err := s.db.GetLatestConversation(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.activeConvID = convID
	s.mu.Unlock()

	// Reset heartbeat for the new project.
	s.mu.Lock()
	s.heartbeat = HeartbeatState{
		IsHealthy:   true,
		Phase:       "Idle",
		Breadcrumbs: []string{"Idle"},
		UpdatedAt:   time.Now(),
	}
	if s.orch != nil {
		s.heartbeat.VRAMProfile = string(s.orch.VRAMProfile())
	}
	s.mu.Unlock()

	return nil
}

// GetActiveConversation returns the conversation ID for the active project.
func (s *WarRoomService) GetActiveConversation(ctx context.Context) (string, error) {
	s.mu.RLock()
	cached := s.activeConvID
	s.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}
	if s.db == nil {
		return "", nil
	}
	p, err := s.db.GetActiveProject(ctx)
	if err != nil || p == nil {
		return "", err
	}
	convID, err := s.db.GetLatestConversation(ctx, p.ID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.activeConvID = convID
	s.mu.Unlock()
	return convID, nil
}

// GetMessages returns messages for a conversation, newest last.
// Pass limit ≤ 0 to return all.
func (s *WarRoomService) GetMessages(ctx context.Context, conversationID string, limit int) ([]models.Message, error) {
	if s.db == nil || conversationID == "" {
		return nil, nil
	}
	return s.db.ListMessagesByConversation(ctx, conversationID, limit)
}

// SendBossCommand enqueues a Boss instruction to the Orchestrator.
// Returns immediately; responses arrive via kotui:message events.
func (s *WarRoomService) SendBossCommand(ctx context.Context, command string) error {
	if s.orch == nil {
		return fmt.Errorf("orchestrator not initialised — is Ollama running?")
	}
	go func() {
		if err := s.orch.HandleBossCommand(context.Background(), command); err != nil {
			s.app.Event.Emit("kotui:error", map[string]string{"error": err.Error()})
		}
	}()
	return nil
}

// GetAgents returns the current agent roster for the active project.
func (s *WarRoomService) GetAgents(ctx context.Context) ([]AgentInfo, error) {
	// Always include the Lead as a synthetic entry.
	infos := []AgentInfo{{
		ID:     "lead",
		Name:   "Lead",
		Role:   "lead",
		Status: "idle",
		Model:  "",
	}}
	if s.db == nil {
		return infos, nil
	}
	p, err := s.db.GetActiveProject(ctx)
	if err != nil || p == nil {
		return infos, nil
	}
	agents, err := s.db.ListAgents(ctx, p.ID)
	if err != nil {
		return infos, err
	}
	for _, a := range agents {
		if a.ID == "lead" {
			infos[0].Model = a.Model
			infos[0].Status = string(a.Status)
			continue
		}
		infos = append(infos, AgentInfo{
			ID:     a.ID,
			Name:   a.Name,
			Role:   string(a.Role),
			Status: string(a.Status),
			Model:  a.Model,
		})
	}
	return infos, nil
}

// GetHeartbeat returns the current heartbeat state snapshot.
func (s *WarRoomService) GetHeartbeat() HeartbeatState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hb := s.heartbeat
	if s.orch != nil {
		hb.VRAMProfile = string(s.orch.VRAMProfile())
	}
	return hb
}
