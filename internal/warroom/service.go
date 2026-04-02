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
	"sync"
	"time"

	"github.com/BurntSushi/toml"
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
func (s *WarRoomService) ListOllamaModels(ctx context.Context, endpoint string) ([]string, error) {
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
// Uses the configured local endpoint. Pull can take several minutes.
func (s *WarRoomService) PullOllamaModel(ctx context.Context, name string) error {
	cl := s.ollamaClient("")
	return cl.PullModel(ctx, name)
}

// DeleteOllamaModel deletes a locally-stored model by name.
func (s *WarRoomService) DeleteOllamaModel(ctx context.Context, name string) error {
	cl := s.ollamaClient("")
	return cl.DeleteModel(ctx, name)
}

// GetCompanyIdentity returns the content of COMPANY_IDENTITY.md.
func (s *WarRoomService) GetCompanyIdentity(ctx context.Context) (string, error) {
	data, err := os.ReadFile(s.companyIdentityPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("warroom: read company identity: %w", err)
	}
	return string(data), nil
}

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

// GetOrCreateDirectConversation returns or creates a DM conversation for the given agent.
func (s *WarRoomService) GetOrCreateDirectConversation(ctx context.Context, agentID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not initialised")
	}
	p, err := s.db.GetActiveProject(ctx)
	if err != nil || p == nil {
		return "", fmt.Errorf("no active project")
	}
	title := "dm:" + agentID
	convID, err := s.db.GetConversationByTitle(ctx, p.ID, title)
	if err != nil {
		return "", err
	}
	if convID != "" {
		return convID, nil
	}
	return s.db.CreateConversation(ctx, p.ID, title)
}

// SendDirectMessage sends a direct feedback message to a specific agent.
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
	msg := models.Message{
		ProjectID:      p.ID,
		ConversationID: convID,
		AgentID:        "boss",
		Kind:           models.KindBossCommand,
		Tier:           models.TierSummary,
		Content:        message,
		CreatedAt:      time.Now(),
	}
	if s.db != nil {
		_ = s.db.SaveMessage(ctx, msg)
	}
	s.app.Event.Emit("kotui:message", msg)
	// Index as boss feedback for recall.
	if s.mem != nil {
		s.mem.IndexAsync(ctx, agentID, p.ID, message, true)
	}
	go func() {
		if err := s.orch.HandleBossCommand(context.Background(), "[DM to "+agentID+"] "+message); err != nil {
			s.app.Event.Emit("kotui:error", map[string]string{"error": err.Error()})
		}
	}()
	return nil
}
