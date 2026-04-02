package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/store"
	"github.com/haepapa/kotui/pkg/models"
)

// openTestDB is a test helper that creates a temporary in-memory-ish SQLite DB.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- Project CRUD --------------------------------------------------------

func TestProjectCreateAndGet(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p := models.Project{
		ID:          "p1",
		Name:        "Alpha",
		Description: "First project",
		DataPath:    "/tmp/alpha",
		Active:      true,
	}
	if err := db.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := db.GetProject(ctx, "p1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "Alpha" {
		t.Errorf("Name: want Alpha, got %q", got.Name)
	}
	if !got.Active {
		t.Error("expected project to be active")
	}
}

func TestSetActiveProject(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p1 := models.Project{ID: "p1", Name: "P1", DataPath: "/tmp/p1", Active: true}
	p2 := models.Project{ID: "p2", Name: "P2", DataPath: "/tmp/p2", Active: false}
	db.CreateProject(ctx, p1)
	db.CreateProject(ctx, p2)

	if err := db.SetActiveProject(ctx, "p2"); err != nil {
		t.Fatalf("SetActiveProject: %v", err)
	}

	active, err := db.GetActiveProject(ctx)
	if err != nil {
		t.Fatalf("GetActiveProject: %v", err)
	}
	if active == nil || active.ID != "p2" {
		t.Errorf("expected p2 active, got %v", active)
	}

	old, _ := db.GetProject(ctx, "p1")
	if old.Active {
		t.Error("p1 should no longer be active")
	}
}

func TestGetActiveProjectNone(t *testing.T) {
	db := openTestDB(t)
	active, err := db.GetActiveProject(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active != nil {
		t.Errorf("expected nil, got %+v", active)
	}
}

// --- Message persistence -------------------------------------------------

func TestSaveAndListMessages(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Need a project and conversation first (FK constraints).
	db.CreateProject(ctx, models.Project{ID: "proj1", Name: "TestProj", DataPath: "/tmp", Active: true})
	convID, err := db.CreateConversation(ctx, "proj1", "chat 1")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	for i := 0; i < 3; i++ {
		msg := models.Message{
			ProjectID:      "proj1",
			ConversationID: convID,
			Kind:           models.KindAgentMessage,
			Tier:           models.TierSummary,
			Content:        "hello",
		}
		if err := db.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
	}

	msgs, err := db.ListMessagesByConversation(ctx, convID, 0)
	if err != nil {
		t.Fatalf("ListMessagesByConversation: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Tier != models.TierSummary {
			t.Errorf("wrong tier: %q", m.Tier)
		}
	}
}

func TestListSummaryMessages(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	db.CreateProject(ctx, models.Project{ID: "p1", Name: "P1", DataPath: "/tmp", Active: true})
	convID, _ := db.CreateConversation(ctx, "p1", "c1")

	before := time.Now()

	db.SaveMessage(ctx, models.Message{ProjectID: "p1", ConversationID: convID, Kind: models.KindAgentMessage, Tier: models.TierSummary, Content: "a"})
	db.SaveMessage(ctx, models.Message{ProjectID: "p1", ConversationID: convID, Kind: models.KindAgentMessage, Tier: models.TierRaw, Content: "b"})
	db.SaveMessage(ctx, models.Message{ProjectID: "p1", ConversationID: convID, Kind: models.KindAgentMessage, Tier: models.TierSummary, Content: "c"})

	msgs, err := db.ListSummaryMessages(ctx, "p1", before)
	if err != nil {
		t.Fatalf("ListSummaryMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 summary messages, got %d", len(msgs))
	}
}

// --- Task tree -----------------------------------------------------------

func TestTaskTree(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	db.CreateProject(ctx, models.Project{ID: "p1", Name: "P1", DataPath: "/tmp", Active: true})

	parent := models.Task{ID: "t-root", ProjectID: "p1", Title: "Root Task"}
	if err := db.CreateTask(ctx, parent); err != nil {
		t.Fatalf("CreateTask (parent): %v", err)
	}

	for _, id := range []string{"t-child-1", "t-child-2"} {
		child := models.Task{ID: id, ProjectID: "p1", ParentID: "t-root", Title: "Sub " + id}
		if err := db.CreateTask(ctx, child); err != nil {
			t.Fatalf("CreateTask (child %s): %v", id, err)
		}
	}

	children, err := db.ListSubTasks(ctx, "t-root")
	if err != nil {
		t.Fatalf("ListSubTasks: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 sub-tasks, got %d", len(children))
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	db.CreateProject(ctx, models.Project{ID: "p1", Name: "P1", DataPath: "/tmp", Active: true})
	db.CreateTask(ctx, models.Task{ID: "t1", ProjectID: "p1", Title: "T1"})

	if err := db.UpdateTaskStatus(ctx, "t1", "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	got, _ := db.GetTask(ctx, "t1")
	if got.Status != "in_progress" {
		t.Errorf("expected in_progress, got %q", got.Status)
	}
}

// --- Approval workflow ---------------------------------------------------

func TestApprovalWorkflow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	db.CreateProject(ctx, models.Project{ID: "p1", Name: "P1", DataPath: "/tmp", Active: true})

	a := models.Approval{
		ID:          "appr-1",
		ProjectID:   "p1",
		Kind:        "hiring",
		SubjectID:   "agent-abc",
		Description: "Onboard the new specialist",
	}
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	pending, err := db.ListPendingApprovals(ctx, "p1")
	if err != nil {
		t.Fatalf("ListPendingApprovals: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("expected 1 pending approval, got %d", len(pending))
	}

	if err := db.DecideApproval(ctx, "appr-1", "approved"); err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}

	got, err := db.GetApproval(ctx, "appr-1")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != "approved" {
		t.Errorf("expected approved, got %q", got.Status)
	}
	if got.DecidedAt == nil {
		t.Error("expected decided_at to be set")
	}

	// Pending list should now be empty.
	pending2, _ := db.ListPendingApprovals(ctx, "p1")
	if len(pending2) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending2))
	}
}

// --- StorePersister round-trip -------------------------------------------

func TestStorePersisterRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	db.CreateProject(ctx, models.Project{ID: "p1", Name: "P1", DataPath: "/tmp", Active: true})
	convID, _ := db.CreateConversation(ctx, "p1", "persist-test")

	d := dispatcher.New()
	persister := store.NewStorePersister(db, nil)
	d.Subscribe("", persister.Handle)

	d.SetProject("p1")
	d.Dispatch(models.Message{
		ConversationID: convID,
		Kind:           models.KindMilestone,
		Tier:           models.TierSummary,
		Content:        "dispatch → store round trip",
	})

	// Give the synchronous handler time (it's direct, so no sleep needed, but be safe).
	count, err := db.CountMessages(ctx, convID)
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 persisted message, got %d", count)
	}
}

// --- RenameProject & ArchiveProject ---------------------------------------

func TestRenameProject(t *testing.T) {
db := openTestDB(t)
ctx := context.Background()

db.CreateProject(ctx, models.Project{ID: "r1", Name: "Original", DataPath: "/tmp", Active: true})

if err := db.RenameProject(ctx, "r1", "Renamed", "new desc"); err != nil {
t.Fatalf("RenameProject: %v", err)
}
p, err := db.GetProject(ctx, "r1")
if err != nil {
t.Fatalf("GetProject: %v", err)
}
if p.Name != "Renamed" {
t.Errorf("expected name 'Renamed', got %q", p.Name)
}
}

func TestRenameProjectEmptyName(t *testing.T) {
db := openTestDB(t)
ctx := context.Background()

db.CreateProject(ctx, models.Project{ID: "r2", Name: "Keep", DataPath: "/tmp", Active: true})
if err := db.RenameProject(ctx, "r2", "", ""); err == nil {
t.Fatal("expected error for empty name, got nil")
}
}

func TestArchiveProject(t *testing.T) {
db := openTestDB(t)
ctx := context.Background()

db.CreateProject(ctx, models.Project{ID: "a1", Name: "A1", DataPath: "/tmp", Active: true})
db.CreateProject(ctx, models.Project{ID: "a2", Name: "A2", DataPath: "/tmp", Active: false})

if err := db.ArchiveProject(ctx, "a1"); err != nil {
t.Fatalf("ArchiveProject: %v", err)
}

projects, err := db.ListProjects(ctx)
if err != nil {
t.Fatalf("ListProjects: %v", err)
}
for _, p := range projects {
if p.ID == "a1" {
t.Errorf("archived project a1 should not appear in ListProjects")
}
}
if len(projects) != 1 || projects[0].ID != "a2" {
t.Errorf("expected only a2 in list, got %v", projects)
}
}
