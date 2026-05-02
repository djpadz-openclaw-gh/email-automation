package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/djpadz/email-automation/internal/models"
)

// mockDB implements the minimal interface needed for scheduler testing.
type mockDB struct {
	actions  []models.DeferredAction
	executed []int64
}

func (m *mockDB) GetPendingDeferredActions(ctx context.Context) ([]models.DeferredAction, error) {
	var pending []models.DeferredAction
	now := time.Now()
	for _, a := range m.actions {
		if a.ExecuteAt.Before(now) && !a.Executed {
			pending = append(pending, a)
		}
	}
	return pending, nil
}

func (m *mockDB) MarkDeferredActionExecuted(ctx context.Context, id int64) error {
	m.executed = append(m.executed, id)
	for i := range m.actions {
		if m.actions[i].ID == id {
			m.actions[i].Executed = true
		}
	}
	return nil
}

func (m *mockDB) CreateDeferredAction(ctx context.Context, action *models.DeferredAction) error {
	action.ID = int64(len(m.actions) + 1)
	m.actions = append(m.actions, *action)
	return nil
}

func TestDeferredAction_ScheduleAndRetrieve(t *testing.T) {
	db := &mockDB{}

	// Schedule an action for 1 second from now
	action := &models.DeferredAction{
		RuleID:    1,
		AccountID: 1,
		MessageID: "test-msg-001",
		Action:    "move",
		Target:    "@Later",
		ExecuteAt: time.Now().Add(1 * time.Second),
	}

	err := db.CreateDeferredAction(context.Background(), action)
	if err != nil {
		t.Fatalf("failed to create deferred action: %v", err)
	}

	if action.ID != 1 {
		t.Errorf("expected ID 1, got %d", action.ID)
	}

	// Before execution time — should return nothing
	pending, err := db.GetPendingDeferredActions(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending actions: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending actions before execute time, got %d", len(pending))
	}

	// Wait for execution time
	time.Sleep(1100 * time.Millisecond)

	// After execution time — should return the action
	pending, err = db.GetPendingDeferredActions(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending actions: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending action after execute time, got %d", len(pending))
	}
	if pending[0].Action != "move" {
		t.Errorf("expected action 'move', got '%s'", pending[0].Action)
	}
	if pending[0].Target != "@Later" {
		t.Errorf("expected target '@Later', got '%s'", pending[0].Target)
	}

	// Mark as executed
	err = db.MarkDeferredActionExecuted(context.Background(), pending[0].ID)
	if err != nil {
		t.Fatalf("failed to mark action executed: %v", err)
	}
	if len(db.executed) != 1 || db.executed[0] != 1 {
		t.Errorf("expected executed IDs [1], got %v", db.executed)
	}

	// After marking executed, should return nothing
	pending, err = db.GetPendingDeferredActions(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending after execution: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after execution, got %d", len(pending))
	}
}

func TestDeferredAction_MultipleActions(t *testing.T) {
	db := &mockDB{}

	// Schedule multiple actions at different times
	actions := []models.DeferredAction{
		{RuleID: 1, AccountID: 1, MessageID: "msg-1", Action: "move", Target: "@A", ExecuteAt: time.Now().Add(-1 * time.Second)},
		{RuleID: 2, AccountID: 1, MessageID: "msg-2", Action: "delete", Target: "", ExecuteAt: time.Now().Add(-1 * time.Second)},
		{RuleID: 3, AccountID: 1, MessageID: "msg-3", Action: "move", Target: "@B", ExecuteAt: time.Now().Add(1 * time.Hour)},
	}

	for i := range actions {
		err := db.CreateDeferredAction(context.Background(), &actions[i])
		if err != nil {
			t.Fatalf("failed to create action %d: %v", i, err)
		}
	}

	// Should return only the 2 past-due actions
	pending, err := db.GetPendingDeferredActions(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending actions, got %d", len(pending))
	}
}

func TestDeferredAction_DeleteAfter(t *testing.T) {
	db := &mockDB{}

	// Simulate a delete_after(3600) action
	action := &models.DeferredAction{
		RuleID:    1,
		AccountID: 1,
		MessageID: "msg-delete",
		Action:    "delete",
		Target:    "",
		ExecuteAt: time.Now().Add(-1 * time.Minute), // Already past due
	}

	err := db.CreateDeferredAction(context.Background(), action)
	if err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	pending, err := db.GetPendingDeferredActions(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].Action != "delete" {
		t.Errorf("expected delete action, got %s", pending[0].Action)
	}
}

func TestDeferredAction_MoveAfter(t *testing.T) {
	db := &mockDB{}

	// Simulate a move_after("@Archive", 7200) action
	action := &models.DeferredAction{
		RuleID:    1,
		AccountID: 1,
		MessageID: "msg-move-later",
		Action:    "move",
		Target:    "@Archive",
		ExecuteAt: time.Now().Add(-1 * time.Minute),
	}

	err := db.CreateDeferredAction(context.Background(), action)
	if err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	pending, err := db.GetPendingDeferredActions(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].Target != "@Archive" {
		t.Errorf("expected target '@Archive', got '%s'", pending[0].Target)
	}
}
