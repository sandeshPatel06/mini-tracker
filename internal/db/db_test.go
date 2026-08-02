package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "minitracker-db-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	database, err := Open(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open database: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, cleanup
}

func TestDatabaseMigrationAndDefaults(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Default organization (ID 1) should exist
	org, err := database.GetOrganization(1)
	if err != nil {
		t.Fatalf("expected default org, got error: %v", err)
	}
	if org.Name != "Default Beta Org" {
		t.Errorf("expected org name 'Default Beta Org', got '%s'", org.Name)
	}
}

func TestOrganizationCRUD(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Create org
	org, err := database.CreateOrganization("Test Corp", "test-corp")
	if err != nil {
		t.Fatalf("failed to create organization: %v", err)
	}
	if org.ID == 0 {
		t.Errorf("expected valid org ID, got 0")
	}

	// Fetch org by ID
	fetched, err := database.GetOrganization(org.ID)
	if err != nil {
		t.Fatalf("failed to get organization: %v", err)
	}
	if fetched.Name != "Test Corp" || fetched.Slug != "test-corp" {
		t.Errorf("unexpected org details: %+v", fetched)
	}

	// Fetch org by Slug
	bySlug, err := database.GetOrganizationBySlug("test-corp")
	if err != nil {
		t.Fatalf("failed to get organization by slug: %v", err)
	}
	if bySlug.ID != org.ID || bySlug.Name != "Test Corp" {
		t.Errorf("unexpected org by slug: %+v", bySlug)
	}
}

func TestUserAndTeamManagement(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	org, err := database.CreateOrganization("Dev Corp", "dev-corp")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	// Create User
	user, err := database.CreateUser(org.ID, "user@devcorp.com", "hashedpass123", "Alice Developer", "owner")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if user.Email != "user@devcorp.com" || user.Role != "owner" {
		t.Errorf("unexpected user values: %+v", user)
	}

	// Get user by email
	byEmail, err := database.GetUserByEmail("user@devcorp.com")
	if err != nil {
		t.Fatalf("failed to get user by email: %v", err)
	}
	if byEmail.ID != user.ID || byEmail.FullName != "Alice Developer" {
		t.Errorf("mismatched user from GetUserByEmail: %+v", byEmail)
	}

	// Get team members
	members, err := database.GetOrgMembers(org.ID)
	if err != nil {
		t.Fatalf("failed to get org members: %v", err)
	}
	if len(members) != 1 || members[0].Email != "user@devcorp.com" {
		t.Errorf("expected 1 team member, got %d", len(members))
	}
}

func TestInvitationFlow(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	org, _ := database.CreateOrganization("Invite Corp", "invite-corp")

	// Create Invitation
	inv, err := database.CreateInvitation(org.ID, "newmember@invitecorp.com", "member", "token_abc123", time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("failed to create invitation: %v", err)
	}
	if inv.Status != "pending" || inv.Token != "token_abc123" {
		t.Errorf("unexpected invitation state: %+v", inv)
	}

	// Fetch invitation by token
	fetchedInv, err := database.GetInvitationByToken("token_abc123")
	if err != nil {
		t.Fatalf("failed to get invitation by token: %v", err)
	}
	if fetchedInv.Email != "newmember@invitecorp.com" {
		t.Errorf("unexpected invitation email: %s", fetchedInv.Email)
	}

	// Accept Invitation
	user, err := database.AcceptInvitation("token_abc123", "Bob Member", "hashedpass456")
	if err != nil {
		t.Fatalf("failed to accept invitation: %v", err)
	}
	if user.Email != "newmember@invitecorp.com" || user.OrgID != org.ID {
		t.Errorf("unexpected accepted user: %+v", user)
	}

	// Verify status updated to accepted
	updatedInv, err := database.GetInvitationByToken("token_abc123")
	if err != nil {
		t.Fatalf("failed to fetch updated invitation: %v", err)
	}
	if updatedInv.Status != "accepted" {
		t.Errorf("expected status 'accepted', got '%s'", updatedInv.Status)
	}

	// Duplicate acceptance should fail
	_, err = database.AcceptInvitation("token_abc123", "Bob Member", "hashedpass456")
	if err == nil {
		t.Errorf("expected error accepting already accepted invitation")
	}
}

func TestLogEntryCRUD(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now()
	entry := &LogEntry{
		Timestamp:    now,
		ImagePath:    filepath.Join(os.TempDir(), "screenshot.png"),
		TotalKeys:    120,
		UniqueKeys:   25,
		EntropyScore: 45.5,
	}

	id, err := database.InsertLog(entry)
	if err != nil {
		t.Fatalf("failed to insert log: %v", err)
	}
	if id == 0 {
		t.Errorf("expected non-zero log ID")
	}

	// Verify unanalyzed log query
	unanalyzed, err := database.GetUnanalyzedLogs()
	if err != nil {
		t.Fatalf("failed to get unanalyzed logs: %v", err)
	}
	if len(unanalyzed) != 1 || unanalyzed[0].ID != id {
		t.Errorf("expected 1 unanalyzed log, got %d", len(unanalyzed))
	}

	// Update AI Result
	err = database.UpdateAIResult(id, "Coding", true, 95.0, "Active Golang development in IDE")
	if err != nil {
		t.Fatalf("failed to update AI result: %v", err)
	}

	// Verify unanalyzed log is now empty
	unanalyzedAfter, _ := database.GetUnanalyzedLogs()
	if len(unanalyzedAfter) != 0 {
		t.Errorf("expected 0 unanalyzed logs after update, got %d", len(unanalyzedAfter))
	}

	// Fetch logs for date
	dateStr := now.Format("2006-01-02")
	logs, err := database.GetLogsForDate(dateStr)
	if err != nil {
		t.Fatalf("failed to get logs for date: %v", err)
	}
	if len(logs) != 1 || logs[0].AICategory != "Coding" || !logs[0].IsProductive {
		t.Errorf("unexpected logs returned: %+v", logs)
	}

	// Get productivity stats
	stats, err := database.GetProductivityStats(dateStr)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalMinutes != 1 || stats.ProductiveMin != 1 || stats.TopCategory != "Coding" {
		t.Errorf("unexpected stats: %+v", stats)
	}
}
