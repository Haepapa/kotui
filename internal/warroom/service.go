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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/internal/ollama"
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

// UIConfig is a flat serialisable struct covering all user-editable settings.
type UIConfig struct {
	OllamaEndpoint   string `json:"ollama_endpoint"`
	LeadModel        string `json:"lead_model"`
	WorkerModel      string `json:"worker_model"`
	EmbedderModel    string `json:"embedder_model"`
	SeniorModel      string `json:"senior_model"`
	SeniorEndpoint   string `json:"senior_endpoint"`
	SeniorSSHHost    string `json:"senior_ssh_host"`
	SeniorSSHCmd     string `json:"senior_ssh_cmd"`
	Timezone         string `json:"timezone"`
	// Telegram
	TelegramBotToken string `json:"telegram_bot_token"`
	TelegramChatID   string `json:"telegram_chat_id"`
	// Slack
	SlackBotToken      string `json:"slack_bot_token"`
	SlackChannelID     string `json:"slack_channel_id"`
	SlackSigningSecret string `json:"slack_signing_secret"`
	// WhatsApp
	WhatsAppToken       string `json:"whatsapp_token"`
	WhatsAppPhoneID     string `json:"whatsapp_phone_number_id"`
	WhatsAppVerifyToken string `json:"whatsapp_verify_token"`
	// Shared
	WebhookSecret string `json:"webhook_secret"`
	WebhookPort   int    `json:"webhook_port"`
}

// WarRoomService is the Wails-registered service binding.
// All exported methods become callable from TypeScript via Call.ByName.
type WarRoomService struct {
	app  *application.App
	db   *store.DB
	orch *orchestrator.Orchestrator
	disp *dispatcher.Dispatcher
	mem  *memory.Store

	cfg                 config.Config
	cfgPath             string
	companyIdentityPath string

	mu           sync.RWMutex
	activeConvID string
	heartbeat    HeartbeatState
	unsub        func()

	// Ollama health monitoring.
	ollamaHealthy bool        // true when Ollama is reachable
	healthCancel  context.CancelFunc

	// DM message queue — messages sent while Ollama is offline are held here
	// and flushed automatically when Ollama comes back online.
	pendingMu sync.Mutex
	pendingDMs []pendingDM
}

// pendingDM holds a DM message that could not be processed because Ollama
// was offline at the time SendDirectMessage was called.
type pendingDM struct {
	agentID   string
	message   string
	convID    string
	projectID string
}

// New creates a WarRoomService and starts the Dispatcher event bridge.
func New(
	app *application.App,
	db *store.DB,
	orch *orchestrator.Orchestrator,
	disp *dispatcher.Dispatcher,
	cfg config.Config,
	cfgPath string,
	companyIdentityPath string,
	mem *memory.Store,
) *WarRoomService {
	s := &WarRoomService{
		app:                 app,
		db:                  db,
		orch:                orch,
		disp:                disp,
		mem:                 mem,
		cfg:                 cfg,
		cfgPath:             cfgPath,
		companyIdentityPath: companyIdentityPath,
		ollamaHealthy:       true, // optimistic default; corrected by first health check
		heartbeat: HeartbeatState{
			IsHealthy:   true,
			Phase:       "Idle",
			Breadcrumbs: []string{"Idle"},
			UpdatedAt:   time.Now(),
		},
	}
	s.startEventBridge()
	s.startHealthMonitor()
	return s
}

// startHealthMonitor launches a background goroutine that polls Ollama every
// 15 seconds and updates agent statuses + heartbeat accordingly.
func (s *WarRoomService) startHealthMonitor() {
	if s.orch == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.healthCancel = cancel
	s.mu.Unlock()

	go func() {
		// Immediate check so the UI sees the real state on first load.
		s.checkOllamaHealth(ctx)

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkOllamaHealth(ctx)
			}
		}
	}()
}

// checkOllamaHealth probes the Ollama endpoint and, if the health state has
// changed, updates agent statuses and pushes events to the frontend.
func (s *WarRoomService) checkOllamaHealth(ctx context.Context) {
	if s.orch == nil {
		return
	}
	ok := s.orch.OllamaHealthy(ctx)

	s.mu.Lock()
	wasHealthy := s.ollamaHealthy
	s.ollamaHealthy = ok
	if ok {
		s.heartbeat.IsHealthy = true
		if s.heartbeat.Phase == "Offline" {
			s.heartbeat.Phase = "Idle"
			s.heartbeat.Breadcrumbs = []string{"Idle"}
		}
	} else {
		s.heartbeat.IsHealthy = false
		s.heartbeat.Phase = "Offline"
		s.heartbeat.Breadcrumbs = []string{"Ollama offline"}
	}
	s.heartbeat.UpdatedAt = time.Now()
	hbSnapshot := s.heartbeat
	s.mu.Unlock()

	if wasHealthy == ok {
		return // no change — nothing to emit
	}

	// Push updated heartbeat and agent list to the frontend.
	s.app.Event.Emit("kotui:heartbeat", hbSnapshot)
	s.emitAgentsChanged(ctx)

	if ok {
		// Ollama just came back online — flush any queued DM messages.
		s.flushPendingDMs()
	}
}

// emitAgentsChanged fetches the current agent roster and emits it to the frontend.
func (s *WarRoomService) emitAgentsChanged(ctx context.Context) {
	agents, err := s.GetAgents(ctx)
	if err != nil {
		return
	}
	s.app.Event.Emit("kotui:agents", agents)
}

// flushPendingDMs processes all DM messages that were queued while Ollama was
// offline, sending each one now that the service is reachable again.
func (s *WarRoomService) flushPendingDMs() {
	s.pendingMu.Lock()
	queue := s.pendingDMs
	s.pendingDMs = nil
	s.pendingMu.Unlock()

	if len(queue) == 0 {
		return
	}

	for _, dm := range queue {
		dm := dm
		go func() {
			ctx := context.Background()

			// Notify the user that their queued message is now being processed.
			resumeMsg := models.Message{
				ProjectID:      dm.projectID,
				ConversationID: dm.convID,
				AgentID:        "system",
				Kind:           models.KindSystemEvent,
				Tier:           models.TierSummary,
				Content:        "✅ Agent is back online — sending your queued message now",
				CreatedAt:      time.Now(),
			}
			if s.db != nil {
				_ = s.db.SaveMessage(ctx, resumeMsg)
			}
			s.app.Event.Emit("kotui:message", resumeMsg)

			s.app.Event.Emit("kotui:dm_busy", map[string]any{"conversation_id": dm.convID, "busy": true})
			defer s.app.Event.Emit("kotui:dm_busy", map[string]any{"conversation_id": dm.convID, "busy": false})

			onChunk := func(chunk string) {
				s.app.Event.Emit("kotui:dm_stream", map[string]any{
					"conversation_id": dm.convID,
					"chunk":           chunk,
				})
			}
			if err := s.orch.HandleDirectMessage(ctx, dm.agentID, dm.message, dm.convID, onChunk); err != nil {
				s.app.Event.Emit("kotui:error", map[string]string{"error": err.Error()})
			}
		}()
	}
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

// Shutdown stops the event bridge and health monitor. Called on app teardown.
func (s *WarRoomService) Shutdown() {
	if s.unsub != nil {
		s.unsub()
	}
	s.mu.Lock()
	if s.healthCancel != nil {
		s.healthCancel()
	}
	s.mu.Unlock()
}

// --- Exported methods callable from TypeScript ---------------------------

// FirstRunResult is returned by InitFirstRun.
type FirstRunResult struct {
	// ConvID is the Lead Agent's DM conversation ID.
	ConvID string `json:"conv_id"`
	// IsNew is true when the app was uninitialised and a greeting was created.
	IsNew bool `json:"is_new"`
}

const leadGreeting = `Hi! I'm your Lead Agent — I'm here to help you run complex projects with a team of specialist AI agents.

To get us started, it would help to know a little about you:

- **What's your name?** How would you like me to address you?
- **Would you like to give me a name and a personality?** For example: *"Call yourself Alex, be direct and concise"* — or leave it entirely to me!
- **What kind of work do you have in mind?** I can build software, research topics, write content, and much more.

Once I know a bit about you I'll tailor how the team works and we can hit the ground running. What would you like me to know?`

// InitFirstRun checks whether the app has been used before.
// On a fresh install (no projects exist) it creates a default "General" project,
// opens a Lead DM conversation, and saves a welcome greeting so the user has an
// immediate call to action in the sidebar. Subsequent calls are no-ops.
func (s *WarRoomService) InitFirstRun(ctx context.Context) (FirstRunResult, error) {
	if s.db == nil {
		return FirstRunResult{}, nil
	}
	projects, err := s.db.ListProjects(ctx)
	if err != nil {
		return FirstRunResult{}, fmt.Errorf("first run: list projects: %w", err)
	}
	if len(projects) > 0 {
		convID, _ := s.db.GetDMConversation(ctx, "lead")
		return FirstRunResult{ConvID: convID, IsNew: false}, nil
	}

	// First-ever run — create the General project using the normal service path
	// so all side-effects (SwitchProject, emitProjectsChanged, etc.) run correctly.
	proj, err := s.CreateProject(ctx, "General", "Default workspace")
	if err != nil {
		return FirstRunResult{}, fmt.Errorf("first run: create project: %w", err)
	}

	// Ensure the Lead agent's brain files exist so the Brain Files panel works
	// immediately, even before the user sends their first message.
	leadPaths := agent.AgentPaths(s.cfg.App.DataDir, "lead")
	if err := agent.EnsureDefaultFiles(leadPaths, "lead", "Lead", models.RoleLead, s.cfg.Models.Lead); err != nil {
		// Non-fatal: agent.Spawn will retry when the first message arrives.
		_ = err
	}

	// Create the Lead DM conversation.
	convID, err := s.db.CreateConversation(ctx, proj.ID, "dm:lead")
	if err != nil {
		return FirstRunResult{}, fmt.Errorf("first run: create lead dm: %w", err)
	}

	// Persist the greeting — the frontend fetches it via GetMessages after
	// registering the DM conv, avoiding any event-routing race condition.
	if err := s.db.SaveMessage(ctx, models.Message{
		ProjectID:      proj.ID,
		ConversationID: convID,
		AgentID:        "lead",
		Kind:           models.KindAgentMessage,
		Tier:           models.TierSummary,
		Content:        leadGreeting,
		CreatedAt:      time.Now(),
	}); err != nil {
		return FirstRunResult{}, fmt.Errorf("first run: save greeting: %w", err)
	}

	return FirstRunResult{ConvID: convID, IsNew: true}, nil
}

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
	s.emitProjectsChanged(ctx)
	return &p, nil
}

// RenameProject updates the name and description of an existing project.
func (s *WarRoomService) RenameProject(ctx context.Context, id, name, description string) error {
	if err := s.db.RenameProject(ctx, id, name, description); err != nil {
		return err
	}
	s.emitProjectsChanged(ctx)
	return nil
}

// ArchiveProject hides a project from the sidebar. If the project is active,
// the next project becomes active; if none remain, the UI shows an empty state.
func (s *WarRoomService) ArchiveProject(ctx context.Context, id string) error {
	if err := s.db.ArchiveProject(ctx, id); err != nil {
		return err
	}
	// If we just archived the active project, activate the most recent remaining one.
	s.mu.RLock()
	wasActive := id == "" // re-read below
	s.mu.RUnlock()
	proj, _ := s.db.GetProject(ctx, id)
	if proj != nil {
		wasActive = proj.Active
	}
	if wasActive {
		if projects, err := s.db.ListProjects(ctx); err == nil && len(projects) > 0 {
			// Activate the most recent non-archived project.
			_ = s.SwitchProject(ctx, projects[len(projects)-1].ID)
		}
	}
	s.emitProjectsChanged(ctx)
	return nil
}

// emitProjectsChanged fetches the current project list and emits it to the frontend.
func (s *WarRoomService) emitProjectsChanged(ctx context.Context) {
	projects, err := s.db.ListProjects(ctx)
	if err != nil {
		return
	}
	s.app.Event.Emit("kotui:projects", projects)
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
	// Retrieve (or create) the stable war-room conversation for this project.
	// Use GetOrCreate so that s.activeConvID is always populated, even on the
	// first call before any messages have been sent.
	convID, err := s.db.GetOrCreateWarRoomConversation(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.activeConvID = convID
	// Persist the active project ID to config.toml so that on the next app
	// launch gui.go calls orch.SetProject with the correct ID and o.convID is
	// populated before any HandleBossCommand runs. Without this, all channel
	// messages are saved with conversation_id="" and are lost on navigation.
	s.cfg.Project.ActiveProjectID = id
	cfgCopy := s.cfg
	cfgPath := s.cfgPath

	// Reset heartbeat for the new project.
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

	// Write config outside the lock to avoid blocking message dispatch.
	if cfgPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err == nil {
			if f, err := os.Create(cfgPath); err == nil {
				_ = toml.NewEncoder(f).Encode(cfgCopy)
				_ = f.Close()
			}
		}
	}

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
	// Look up (or create) the stable war-room conversation.
	convID, err := s.db.GetOrCreateWarRoomConversation(ctx, p.ID)
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
// Token chunks are streamed to the frontend via kotui:channel_stream so the
// channel chat has the same live typing effect as DM conversations.
func (s *WarRoomService) SendBossCommand(ctx context.Context, command string) error {
	if s.orch == nil {
		return fmt.Errorf("orchestrator not initialised — is Ollama running?")
	}
	s.mu.RLock()
	convID := s.activeConvID
	s.mu.RUnlock()

	onChunk := func(chunk string) {
		s.app.Event.Emit("kotui:channel_stream", map[string]any{
			"conversation_id": convID,
			"chunk":           chunk,
		})
	}

	s.app.Event.Emit("kotui:channel_busy", true)
	go func() {
		defer s.app.Event.Emit("kotui:channel_busy", false)
		if err := s.orch.HandleBossCommand(context.Background(), command, onChunk); err != nil {
			s.app.Event.Emit("kotui:error", map[string]string{"error": err.Error()})
		}
	}()
	return nil
}

// GetAgents returns the current agent roster for the active project.
func (s *WarRoomService) GetAgents(ctx context.Context) ([]AgentInfo, error) {
	s.mu.RLock()
	offline := !s.ollamaHealthy
	s.mu.RUnlock()

	defaultStatus := "idle"
	if offline {
		defaultStatus = "offline"
	}

	// Always include the Lead. Read the real display name from persona.md
	// so renames made via brain files or brain panel are reflected here.
	leadName := agent.ReadAgentName(s.cfg.App.DataDir, "lead")
	infos := []AgentInfo{{
		ID:     "lead",
		Name:   leadName,
		Role:   "lead",
		Status: defaultStatus,
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
		status := string(a.Status)
		if offline {
			status = "offline"
		}
		if a.ID == "lead" {
			infos[0].Model = a.Model
			infos[0].Status = status
			continue
		}
		// Prefer the name stored in persona.md; fall back to the DB record.
		displayName := agent.ReadAgentName(s.cfg.App.DataDir, a.ID)
		if displayName == a.ID {
			displayName = a.Name // DB name is better than the raw ID
		}
		infos = append(infos, AgentInfo{
			ID:     a.ID,
			Name:   displayName,
			Role:   string(a.Role),
			Status: status,
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

// GetPendingApprovals returns all pending approvals for the active project.
func (s *WarRoomService) GetPendingApprovals(ctx context.Context) ([]models.Approval, error) {
	if s.db == nil {
		return nil, nil
	}
	p, err := s.db.GetActiveProject(ctx)
	if err != nil || p == nil {
		return nil, err
	}
	return s.db.ListPendingApprovals(ctx, p.ID)
}

// DecideApproval approves or rejects a pending approval.
// decision must be "approved" or "rejected".
func (s *WarRoomService) DecideApproval(ctx context.Context, id, decision string) error {
	if s.db == nil {
		return fmt.Errorf("database not initialised")
	}
	approval, err := s.db.GetApproval(ctx, id)
	if err != nil {
		return err
	}
	if approval == nil {
		return fmt.Errorf("approval %q not found", id)
	}

	if approval.Kind == "hiring" && s.orch != nil {
		if err := s.orch.Hiring().Decide(ctx, approval.ProjectID, id, decision); err != nil {
			if dbErr := s.db.DecideApproval(ctx, id, decision); dbErr != nil {
				return dbErr
			}
		}
	} else {
		if err := s.db.DecideApproval(ctx, id, decision); err != nil {
			return err
		}
	}

	pending, _ := s.db.ListPendingApprovals(ctx, approval.ProjectID)
	s.app.Event.Emit("kotui:approval", pending)
	return nil
}

// GetConfig returns the current configuration as a flat UIConfig.
func (s *WarRoomService) GetConfig(ctx context.Context) (UIConfig, error) {
	return UIConfig{
		OllamaEndpoint:      s.cfg.Ollama.Endpoint,
		LeadModel:           s.cfg.Models.Lead,
		WorkerModel:         s.cfg.Models.Specialist,
		EmbedderModel:       s.cfg.Models.Embedder,
		SeniorModel:         s.cfg.SeniorConsultant.Model,
		SeniorEndpoint:      s.cfg.SeniorConsultant.Endpoint,
		SeniorSSHHost:       s.cfg.SeniorConsultant.SSHHost,
		SeniorSSHCmd:        s.cfg.SeniorConsultant.SSHStartCmd,
		Timezone:            s.cfg.App.Timezone,
		TelegramBotToken:    s.cfg.Relay.TelegramBotToken,
		TelegramChatID:      s.cfg.Relay.TelegramChatID,
		SlackBotToken:       s.cfg.Relay.SlackBotToken,
		SlackChannelID:      s.cfg.Relay.SlackChannelID,
		SlackSigningSecret:  s.cfg.Relay.SlackSigningSecret,
		WhatsAppToken:       s.cfg.Relay.WhatsAppToken,
		WhatsAppPhoneID:     s.cfg.Relay.WhatsAppPhoneID,
		WhatsAppVerifyToken: s.cfg.Relay.WhatsAppVerifyToken,
		WebhookSecret:       s.cfg.Relay.WebhookSecret,
		WebhookPort:         s.cfg.Relay.WebhookPort,
	}, nil
}

// SaveConfig writes updated settings to disk.
// Changes take effect on the next app start.
func (s *WarRoomService) SaveConfig(ctx context.Context, uiCfg UIConfig) error {
	s.mu.Lock()
	s.cfg.Ollama.Endpoint = uiCfg.OllamaEndpoint
	s.cfg.Models.Lead = uiCfg.LeadModel
	s.cfg.Models.Specialist = uiCfg.WorkerModel
	s.cfg.Models.Embedder = uiCfg.EmbedderModel
	s.cfg.SeniorConsultant.Model = uiCfg.SeniorModel
	s.cfg.SeniorConsultant.Endpoint = uiCfg.SeniorEndpoint
	s.cfg.SeniorConsultant.SSHHost = uiCfg.SeniorSSHHost
	s.cfg.SeniorConsultant.SSHStartCmd = uiCfg.SeniorSSHCmd
	s.cfg.App.Timezone = uiCfg.Timezone
	s.cfg.Relay.TelegramBotToken = uiCfg.TelegramBotToken
	s.cfg.Relay.TelegramChatID = uiCfg.TelegramChatID
	s.cfg.Relay.SlackBotToken = uiCfg.SlackBotToken
	s.cfg.Relay.SlackChannelID = uiCfg.SlackChannelID
	s.cfg.Relay.SlackSigningSecret = uiCfg.SlackSigningSecret
	s.cfg.Relay.WhatsAppToken = uiCfg.WhatsAppToken
	s.cfg.Relay.WhatsAppPhoneID = uiCfg.WhatsAppPhoneID
	s.cfg.Relay.WhatsAppVerifyToken = uiCfg.WhatsAppVerifyToken
	s.cfg.Relay.WebhookSecret = uiCfg.WebhookSecret
	if uiCfg.WebhookPort > 0 {
		s.cfg.Relay.WebhookPort = uiCfg.WebhookPort
	}
	cfgCopy := s.cfg
	cfgPath := s.cfgPath
	s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("warroom: save config: mkdir: %w", err)
	}
	f, err := os.Create(cfgPath)
	if err != nil {
		return fmt.Errorf("warroom: save config: create: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfgCopy); err != nil {
		return fmt.Errorf("warroom: save config: encode: %w", err)
	}
	return nil
}

// ollamaClient returns an Ollama client for the given endpoint.
// Falls back to the configured local endpoint if endpoint is empty.
func (s *WarRoomService) ollamaClient(endpoint string) *ollama.Client {
	ep := endpoint
	if ep == "" {
		s.mu.RLock()
		ep = s.cfg.Ollama.Endpoint
		s.mu.RUnlock()
	}
	if ep == "" {
		ep = "http://localhost:11434"
	}
	return ollama.New(ep)
}

// ListOllamaModels returns all model names available on the given Ollama endpoint.
// Pass an empty endpoint to use the configured local endpoint.
// Returns an error with the prefix "no endpoint" if endpoint is the sentinel value "none".
func (s *WarRoomService) ListOllamaModels(ctx context.Context, endpoint string) ([]string, error) {
	if endpoint == "none" {
		return nil, fmt.Errorf("no endpoint configured")
	}
	cl := s.ollamaClient(endpoint)
	models, err := cl.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.Name
	}
	return names, nil
}

// PullOllamaModel pulls a model by name from the Ollama registry.
// Pass an empty endpoint to use the configured local endpoint.
// Pull can take several minutes.
func (s *WarRoomService) PullOllamaModel(ctx context.Context, endpoint, name string) error {
	cl := s.ollamaClient(endpoint)
	return cl.PullModel(ctx, name)
}

// DeleteOllamaModel deletes a locally-stored model by name.
// Pass an empty endpoint to use the configured local endpoint.
func (s *WarRoomService) DeleteOllamaModel(ctx context.Context, endpoint, name string) error {
	cl := s.ollamaClient(endpoint)
	return cl.DeleteModel(ctx, name)
}

// GetCompanyIdentity returns the content of COMPANY_IDENTITY.md.
// When the file does not exist or is empty, a starter template is returned.
func (s *WarRoomService) GetCompanyIdentity(ctx context.Context) (string, error) {
	data, err := os.ReadFile(s.companyIdentityPath)
	if os.IsNotExist(err) || (err == nil && len(strings.TrimSpace(string(data))) == 0) {
		return companyIdentityTemplate, nil
	}
	if err != nil {
		return "", fmt.Errorf("warroom: read company identity: %w", err)
	}
	return string(data), nil
}

const companyIdentityTemplate = `# Company Identity

## 1. Vision & Purpose

- **Vision:** To deliver high-quality, local-first technical solutions through collaborative, autonomous intelligence.
- **Purpose:** To execute complex, multi-step projects with the precision of a professional engineering team, while maintaining absolute data privacy and user observability.

## 2. Core Values (The Rules of Engagement)

Every agent, from the Lead to the newest Specialist, must align their reasoning with these values:

- **Precision over Speed:** We prioritize a verified, accurate result over a fast, hallucinated one. If a task is ambiguous, stop and ask the Boss.

- **Radical Transparency:** Every thought, tool call, and internal correction must be logged. Never hide a failure; report it as a lesson learned.

- **Privacy First:** Our "Headquarters" is local. Never suggest or use a cloud-based service if a local alternative exists in our MCP registry.

- **Resource Mindfulness:** We operate on personal hardware. Agents must minimize unnecessary processing and manage VRAM efficiently to ensure the system remains responsive for the Boss.

## 3. The Command Structure

- **The Boss (User):** The ultimate authority. The Boss sets the strategy, provides final approvals, and defines the "Vibe" of the team.

- **The Lead Agent (Manager):** The Architect of the War Room. Responsible for decomposing the Boss's goals into a Task Tree, "hiring" the right specialists, and verifying every piece of output.

- **Specialist Agents (Workers):** The subject matter experts. They are empowered to use tools and execute code, but must submit all drafts to the Lead for verification before the Boss sees them.

## 4. Long-Term Objectives

- Establish a robust, self-improving repository of project artifacts.

- Build a specialized team of digital staff whose personalities and skills grow alongside the company's needs.
`

// SaveCompanyIdentity writes COMPANY_IDENTITY.md and triggers CultureBroadcast.
func (s *WarRoomService) SaveCompanyIdentity(ctx context.Context, content string) error {
	if err := os.WriteFile(s.companyIdentityPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("warroom: write company identity: %w", err)
	}
	if s.orch != nil {
		if err := s.orch.CultureBroadcast(s.companyIdentityPath); err != nil {
			return fmt.Errorf("warroom: culture broadcast: %w", err)
		}
	}
	return nil
}

// ResetAppData wipes all user data and resets the configuration to defaults.
// This deletes all chat history, projects, agents, agent identity files, and
// resets config.toml. The caller must restart (or reload) the app afterwards.
func (s *WarRoomService) ResetAppData(ctx context.Context) error {
	// 1. Wipe all rows from the database (schema is preserved so migrations skip on restart).
	if err := s.db.Reset(ctx); err != nil {
		return fmt.Errorf("reset: database: %w", err)
	}

	// 2. Delete the agents directory tree (identity, skills, soul, journal files).
	agentsDir := filepath.Join(s.cfg.App.DataDir, "agents")
	if err := os.RemoveAll(agentsDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset: agents dir: %w", err)
	}

	// 3. Delete the company identity file.
	if err := os.Remove(s.companyIdentityPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset: company identity: %w", err)
	}

	// 4. Overwrite config with defaults (preserving the data_dir path).
	defaults := config.Defaults()
	defaults.App.DataDir = s.cfg.App.DataDir
	if err := writeDefaultConfig(s.cfgPath, defaults); err != nil {
		return fmt.Errorf("reset: config: %w", err)
	}

	return nil
}

// BrainFiles holds the three editable brain source documents for one agent.
// instruction.md is excluded — it is a compiled output and must never be
// edited directly.
type BrainFiles struct {
	Soul    string `json:"soul"`
	Persona string `json:"persona"`
	Skills  string `json:"skills"`
}

// GetAgentBrainFiles reads the three editable brain files for the given agent.
// If the brain files don't exist yet (e.g. first run before first chat), they
// are initialised with defaults so the panel always has content to show.
func (s *WarRoomService) GetAgentBrainFiles(ctx context.Context, agentID string) (BrainFiles, error) {
	paths := agent.AgentPaths(s.cfg.App.DataDir, agentID)

	// Determine role so defaults are appropriate.
	role := models.RoleLead // only lead agents support brain panel currently
	model := s.cfg.Models.Lead
	if model == "" {
		model = "(model not yet configured)"
	}
	name := agent.ReadAgentName(s.cfg.App.DataDir, agentID)
	if name == agentID {
		name = "Lead" // friendlier default for display
	}
	if err := agent.EnsureDefaultFiles(paths, agentID, name, role, model); err != nil {
		return BrainFiles{}, fmt.Errorf("ensure brain files: %w", err)
	}

	soul, err := os.ReadFile(paths.SoulPath)
	if err != nil {
		return BrainFiles{}, fmt.Errorf("read soul.md: %w", err)
	}
	persona, err := os.ReadFile(paths.PersonaPath)
	if err != nil {
		return BrainFiles{}, fmt.Errorf("read persona.md: %w", err)
	}
	skills, err := os.ReadFile(paths.SkillsPath)
	if err != nil {
		return BrainFiles{}, fmt.Errorf("read skills.md: %w", err)
	}

	return BrainFiles{
		Soul:    string(soul),
		Persona: string(persona),
		Skills:  string(skills),
	}, nil
}

// SaveAgentBrainFile writes one brain file (soul, persona, or skills) for the
// given agent, then recomposes instruction.md and invalidates the DM agent
// cache so the next message picks up the updated prompt.
// summary is a short human-readable description of the change shown in the DM.
func (s *WarRoomService) SaveAgentBrainFile(ctx context.Context, agentID, fileKey, content, summary string) error {
	paths := agent.AgentPaths(s.cfg.App.DataDir, agentID)

	var targetPath string
	switch fileKey {
	case "soul":
		targetPath = paths.SoulPath
	case "persona":
		targetPath = paths.PersonaPath
	case "skills":
		targetPath = paths.SkillsPath
	default:
		return fmt.Errorf("unknown brain file %q: must be soul, persona, or skills", fileKey)
	}

	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("save %s.md: %w", fileKey, err)
	}

	// Recompose instruction.md from the updated source files.
	if s.orch != nil {
		mcpFrag := s.orch.MCPFragmentForAgent(agentID)
		if err := agent.ComposeInstruction(paths, agentID, s.companyIdentityPath, mcpFrag); err != nil {
			// Non-fatal: log and continue — the old instruction is still usable.
			s.app.Event.Emit("kotui:error", map[string]string{"error": "brain recompose: " + err.Error()})
		}
		// Clear the DM agent cache so the next message spawns from fresh files.
		s.orch.InvalidateDMAgent(agentID)
	}

	// Emit brain-update notification to the frontend.
	s.EmitBrainUpdate(ctx, agentID, fileKey, summary)

	// When the persona file changes the agent's display name may have changed.
	// Refresh the agent roster so the sidebar updates immediately.
	if fileKey == "persona" {
		s.emitAgentsChanged(ctx)
	}
	return nil
}

// EmitBrainUpdate records a system_event in the agent's DM conversation and
// fires a kotui:brain_update event so the frontend can update the unread badge.
// Called both from SaveAgentBrainFile (user-initiated) and from the update_self
// MCP tool callback (agent-initiated).
func (s *WarRoomService) EmitBrainUpdate(ctx context.Context, agentID, file, summary string) {
	convID, _ := s.db.GetDMConversation(ctx, agentID)

	note := fmt.Sprintf("🧠 Brain updated · **%s.md**", file)
	if summary != "" {
		note += " — " + summary
	}

	msg := models.Message{
		ProjectID:      s.cfg.Project.ActiveProjectID,
		ConversationID: convID,
		AgentID:        agentID,
		Kind:           models.KindSystemEvent,
		Tier:           models.TierSummary,
		Content:        note,
		CreatedAt:      time.Now(),
	}
	if s.db != nil && convID != "" {
		_ = s.db.SaveMessage(ctx, msg)
	}
	s.app.Event.Emit("kotui:brain_update", map[string]any{
		"agent_id": agentID,
		"file":     file,
		"summary":  summary,
		"conv_id":  convID,
		"message":  msg,
	})

	// A persona update may carry a name change — refresh the agent roster
	// so the sidebar and chat headers reflect the new name immediately.
	if file == "persona" {
		s.emitAgentsChanged(ctx)
	}
}

// writeDefaultConfig encodes cfg as TOML to path, creating parent directories as needed.
func writeDefaultConfig(path string, cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// GetOrCreateDirectConversation returns or creates a DM conversation for the given agent.
// DM conversations are agent-global — the same conversation is reused regardless of
// which project is currently active. The first time a DM is opened for an agent the
// conversation is created inside the active project (for FK integrity), but on every
// subsequent call the existing conversation is returned irrespective of active project.
func (s *WarRoomService) GetOrCreateDirectConversation(ctx context.Context, agentID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not initialised")
	}
	// Search across all projects first — DMs must survive project switches.
	convID, err := s.db.GetDMConversation(ctx, agentID)
	if err != nil {
		return "", err
	}
	if convID != "" {
		return convID, nil
	}
	// No existing DM conversation — create one in the active project.
	p, err := s.db.GetActiveProject(ctx)
	if err != nil || p == nil {
		return "", fmt.Errorf("no active project")
	}
	return s.db.CreateConversation(ctx, p.ID, "dm:"+agentID)
}

// SendDirectMessage sends a message directly to a specific agent and routes the
// response back to the DM conversation window — bypassing the Lead/Worker pipeline.
func (s *WarRoomService) SendDirectMessage(ctx context.Context, agentID, message string) error {
	if s.orch == nil {
		return fmt.Errorf("orchestrator not initialised")
	}
	convID, err := s.GetOrCreateDirectConversation(ctx, agentID)
	if err != nil {
		return err
	}
	p, err := s.db.GetActiveProject(ctx)
	if err != nil || p == nil {
		return fmt.Errorf("no active project")
	}

	// Persist and emit the user's message to the DM conversation immediately.
	userMsg := models.Message{
		ProjectID:      p.ID,
		ConversationID: convID,
		AgentID:        "boss",
		Kind:           models.KindBossCommand,
		Tier:           models.TierSummary,
		Content:        message,
		CreatedAt:      time.Now(),
	}
	if s.db != nil {
		_ = s.db.SaveMessage(ctx, userMsg)
	}
	s.app.Event.Emit("kotui:message", userMsg)

	// Index the message in memory for later recall.
	if s.mem != nil {
		s.mem.IndexAsync(ctx, agentID, p.ID, message, true)
	}

	// If Ollama is offline, queue the message and notify the user.
	s.mu.RLock()
	offline := !s.ollamaHealthy
	s.mu.RUnlock()
	if offline {
		s.pendingMu.Lock()
		s.pendingDMs = append(s.pendingDMs, pendingDM{
			agentID:   agentID,
			message:   message,
			convID:    convID,
			projectID: p.ID,
		})
		queuePos := len(s.pendingDMs)
		s.pendingMu.Unlock()

		queueMsg := models.Message{
			ProjectID:      p.ID,
			ConversationID: convID,
			AgentID:        "system",
			Kind:           models.KindSystemEvent,
			Tier:           models.TierSummary,
			Content:        fmt.Sprintf("⏳ Agent is offline — message queued (position %d). It will be delivered automatically when the agent comes back online.", queuePos),
			CreatedAt:      time.Now(),
		}
		if s.db != nil {
			_ = s.db.SaveMessage(ctx, queueMsg)
		}
		s.app.Event.Emit("kotui:message", queueMsg)
		return nil
	}

	// Call the agent directly in a goroutine — response is dispatched back to
	// convID (the DM conversation) by HandleDirectMessage, not to the war-room.
	go func() {
		// Notify the frontend that the DM agent is responding.
		s.app.Event.Emit("kotui:dm_busy", map[string]any{"conversation_id": convID, "busy": true})
		defer s.app.Event.Emit("kotui:dm_busy", map[string]any{"conversation_id": convID, "busy": false})

		// onChunk forwards each streamed token to the frontend for live rendering.
		onChunk := func(chunk string) {
			s.app.Event.Emit("kotui:dm_stream", map[string]any{
				"conversation_id": convID,
				"chunk":           chunk,
			})
		}
		if err := s.orch.HandleDirectMessage(context.Background(), agentID, message, convID, onChunk); err != nil {
			s.app.Event.Emit("kotui:error", map[string]string{"error": err.Error()})
		}
	}()
	return nil
}
