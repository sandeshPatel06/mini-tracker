package db

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// LogEntry represents one minute of tracked activity.
type LogEntry struct {
	ID           int64     `json:"id" ts_type:"number"`
	Timestamp    time.Time `json:"timestamp" ts_type:"string"`
	ImagePath    string    `json:"image_path" ts_type:"string"`
	TotalKeys    int       `json:"total_keys" ts_type:"number"`
	UniqueKeys   int       `json:"unique_keys" ts_type:"number"`
	EntropyScore float64   `json:"entropy_score" ts_type:"number"`
	AICategory   string    `json:"ai_category" ts_type:"string"`
	IsProductive bool      `json:"is_productive" ts_type:"boolean"`
	AIConfidence float64   `json:"ai_confidence" ts_type:"number"`
	AIReason     string    `json:"ai_reason" ts_type:"string"`
}

// ProductivityStats holds aggregate stats for a day.
type ProductivityStats struct {
	Date             string  `json:"date"`
	TotalMinutes     int     `json:"total_minutes"`
	ProductiveMin    int     `json:"productive_minutes"`
	UnproductiveMin  int     `json:"unproductive_minutes"`
	AvgEntropyScore  float64 `json:"avg_entropy_score"`
	TopCategory      string  `json:"top_category"`
}

// DB wraps the SQLite connection.
type DB struct {
	conn *sql.DB
}

// Open initialises the SQLite database at the given directory.
func Open(dataDir string) (*DB, error) {
	dbPath := filepath.Join(dataDir, "tracker.db")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Organization represents a corporate tenant.
type Organization struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// User represents a team member.
type User struct {
	ID           int64     `json:"id"`
	OrgID        int64     `json:"org_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	FullName     string    `json:"full_name"`
	Role         string    `json:"role"` // owner, admin, member
	CreatedAt    time.Time `json:"created_at"`
}

// Invitation represents an email invite to join an organization.
type Invitation struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Token     string    `json:"token"`
	Status    string    `json:"status"` // pending, accepted, expired
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS organizations (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			slug        TEXT UNIQUE NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			org_id        INTEGER NOT NULL,
			email         TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			full_name     TEXT NOT NULL,
			role          TEXT CHECK(role IN ('owner', 'admin', 'member')) DEFAULT 'member',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(org_id) REFERENCES organizations(id)
		);

		CREATE TABLE IF NOT EXISTS invitations (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			org_id      INTEGER NOT NULL,
			email       TEXT NOT NULL,
			role        TEXT CHECK(role IN ('admin', 'member')) DEFAULT 'member',
			token       TEXT UNIQUE NOT NULL,
			status      TEXT CHECK(status IN ('pending', 'accepted', 'expired')) DEFAULT 'pending',
			expires_at  DATETIME NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(org_id) REFERENCES organizations(id)
		);

		CREATE TABLE IF NOT EXISTS logs (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			org_id         INTEGER DEFAULT 1,
			user_id        INTEGER DEFAULT 1,
			timestamp      DATETIME NOT NULL,
			image_path     TEXT,
			total_keys     INTEGER DEFAULT 0,
			unique_keys    INTEGER DEFAULT 0,
			entropy_score  REAL DEFAULT 0,
			ai_category    TEXT DEFAULT '',
			is_productive  INTEGER DEFAULT 0,
			ai_confidence  REAL DEFAULT 0,
			ai_reason      TEXT DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);
	`)
	if err != nil {
		return err
	}

	// Seed default organization and test accounts if empty
	var orgCount int
	_ = db.conn.QueryRow(`SELECT COUNT(*) FROM organizations`).Scan(&orgCount)
	if orgCount == 0 {
		adminHash := HashPassword("admin123")
		memberHash := HashPassword("user123")
		_, _ = db.conn.Exec(`INSERT INTO organizations (id, name, slug) VALUES (1, 'Default Beta Org', 'default-beta')`)
		_, _ = db.conn.Exec(`INSERT INTO users (id, org_id, email, password_hash, full_name, role) VALUES (1, 1, 'admin@company.com', ?, 'Beta Admin', 'owner')`, adminHash)
		_, _ = db.conn.Exec(`INSERT INTO users (id, org_id, email, password_hash, full_name, role) VALUES (2, 1, 'user@company.com', ?, 'Test Team Member', 'member')`, memberHash)
	}

	return nil
}

// HashPassword hashes a user password consistently using SHA-256 with a salt.
func HashPassword(password string) string {
	h := sha256.Sum256([]byte("mini-tracker-salt-" + password))
	return fmt.Sprintf("%x", h)
}

// CreateOrganization creates a new corporate entity.
func (db *DB) CreateOrganization(name, slug string) (*Organization, error) {
	res, err := db.conn.Exec(`INSERT INTO organizations (name, slug) VALUES (?, ?)`, name, slug)
	if err != nil {
		return nil, fmt.Errorf("create org: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Organization{ID: id, Name: name, Slug: slug, CreatedAt: time.Now()}, nil
}

// GetOrganization returns an org by ID.
func (db *DB) GetOrganization(id int64) (*Organization, error) {
	var o Organization
	var ts string
	err := db.conn.QueryRow(`SELECT id, name, slug, created_at FROM organizations WHERE id = ?`, id).Scan(&o.ID, &o.Name, &o.Slug, &ts)
	if err != nil {
		return nil, err
	}
	o.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &o, nil
}

// GetOrganizationBySlug returns an org by its unique slug.
func (db *DB) GetOrganizationBySlug(slug string) (*Organization, error) {
	var o Organization
	var ts string
	err := db.conn.QueryRow(`SELECT id, name, slug, created_at FROM organizations WHERE slug = ?`, slug).Scan(&o.ID, &o.Name, &o.Slug, &ts)
	if err != nil {
		return nil, err
	}
	o.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &o, nil
}


// CreateUser creates a team member account.
func (db *DB) CreateUser(orgID int64, email, passwordHash, fullName, role string) (*User, error) {
	res, err := db.conn.Exec(`
		INSERT INTO users (org_id, email, password_hash, full_name, role)
		VALUES (?, ?, ?, ?, ?)`,
		orgID, email, passwordHash, fullName, role,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, OrgID: orgID, Email: email, PasswordHash: passwordHash, FullName: fullName, Role: role, CreatedAt: time.Now()}, nil
}

// GetUserByEmail finds a user by email.
func (db *DB) GetUserByEmail(email string) (*User, error) {
	var u User
	var ts string
	err := db.conn.QueryRow(`
		SELECT id, org_id, email, password_hash, full_name, role, created_at
		FROM users WHERE email = ?`, email).Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role, &ts)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &u, nil
}

// GetUserByID finds a user by ID.
func (db *DB) GetUserByID(id int64) (*User, error) {
	var u User
	var ts string
	err := db.conn.QueryRow(`
		SELECT id, org_id, email, password_hash, full_name, role, created_at
		FROM users WHERE id = ?`, id).Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role, &ts)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &u, nil
}

// CreateInvitation creates an email invite record.
func (db *DB) CreateInvitation(orgID int64, email, role, token string, expiresAt time.Time) (*Invitation, error) {
	// Check if already a member
	var count int
	_ = db.conn.QueryRow(`SELECT COUNT(*) FROM users WHERE org_id = ? AND email = ?`, orgID, email).Scan(&count)
	if count > 0 {
		return nil, fmt.Errorf("user %s is already a member of this organization", email)
	}

	// Update existing invitation if pending
	_, _ = db.conn.Exec(`UPDATE invitations SET status = 'expired' WHERE org_id = ? AND email = ? AND status = 'pending'`, orgID, email)

	res, err := db.conn.Exec(`
		INSERT INTO invitations (org_id, email, role, token, status, expires_at)
		VALUES (?, ?, ?, ?, 'pending', ?)`,
		orgID, email, role, token, expiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("create invitation: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Invitation{
		ID:        id,
		OrgID:     orgID,
		Email:     email,
		Role:      role,
		Token:     token,
		Status:    "pending",
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}, nil
}

// GetInvitationByToken returns an invitation by its token.
func (db *DB) GetInvitationByToken(token string) (*Invitation, error) {
	var inv Invitation
	var expTs, crTs string
	err := db.conn.QueryRow(`
		SELECT id, org_id, email, role, token, status, expires_at, created_at
		FROM invitations WHERE token = ?`, token).Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.Token, &inv.Status, &expTs, &crTs)
	if err != nil {
		return nil, err
	}
	inv.ExpiresAt, _ = time.Parse(time.RFC3339, expTs)
	inv.CreatedAt, _ = time.Parse(time.RFC3339, crTs)

	if inv.Status == "pending" && time.Now().After(inv.ExpiresAt) {
		inv.Status = "expired"
		_, _ = db.conn.Exec(`UPDATE invitations SET status = 'expired' WHERE id = ?`, inv.ID)
	}
	return &inv, nil
}

// AcceptInvitation completes invitation onboarding and creates the user.
func (db *DB) AcceptInvitation(token, fullName, passwordHash string) (*User, error) {
	inv, err := db.GetInvitationByToken(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if inv.Status != "pending" {
		return nil, fmt.Errorf("invitation is %s", inv.Status)
	}

	user, err := db.CreateUser(inv.OrgID, inv.Email, passwordHash, fullName, inv.Role)
	if err != nil {
		return nil, err
	}

	_, _ = db.conn.Exec(`UPDATE invitations SET status = 'accepted' WHERE id = ?`, inv.ID)
	return user, nil
}

// GetOrgMembers lists all members of an organization.
func (db *DB) GetOrgMembers(orgID int64) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT id, org_id, email, full_name, role, created_at
		FROM users WHERE org_id = ? ORDER BY id ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var ts string
		if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.FullName, &u.Role, &ts); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetPendingInvitations lists all active pending invites for an org.
func (db *DB) GetPendingInvitations(orgID int64) ([]Invitation, error) {
	rows, err := db.conn.Query(`
		SELECT id, org_id, email, role, token, status, expires_at, created_at
		FROM invitations WHERE org_id = ? AND status = 'pending' ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invs []Invitation
	for rows.Next() {
		var inv Invitation
		var expTs, crTs string
		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.Token, &inv.Status, &expTs, &crTs); err != nil {
			return nil, err
		}
		inv.ExpiresAt, _ = time.Parse(time.RFC3339, expTs)
		inv.CreatedAt, _ = time.Parse(time.RFC3339, crTs)

		if time.Now().After(inv.ExpiresAt) {
			inv.Status = "expired"
			_, _ = db.conn.Exec(`UPDATE invitations SET status = 'expired' WHERE id = ?`, inv.ID)
			continue
		}
		invs = append(invs, inv)
	}
	return invs, rows.Err()
}

// InsertLog stores a new log entry and returns its ID.
func (db *DB) InsertLog(e *LogEntry) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO logs (timestamp, image_path, total_keys, unique_keys, entropy_score)
		VALUES (?, ?, ?, ?, ?)`,
		e.Timestamp.UTC().Format(time.RFC3339),
		e.ImagePath, e.TotalKeys, e.UniqueKeys, e.EntropyScore,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateAIResult patches the AI fields for a given log ID.
func (db *DB) UpdateAIResult(id int64, category string, productive bool, confidence float64, reason string) error {
	productive_int := 0
	if productive {
		productive_int = 1
	}
	_, err := db.conn.Exec(`
		UPDATE logs SET ai_category=?, is_productive=?, ai_confidence=?, ai_reason=?
		WHERE id=?`,
		category, productive_int, confidence, reason, id,
	)
	return err
}

// GetLogsFiltered returns logs filtered by optional user_id, start_date, and end_date.
func (db *DB) GetLogsFiltered(userID int64, startDate, endDate string) ([]LogEntry, error) {
	query := `SELECT id, timestamp, image_path, total_keys, unique_keys, entropy_score,
		       ai_category, is_productive, ai_confidence, ai_reason
		FROM logs WHERE 1=1`
	args := []interface{}{}

	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	if startDate != "" && endDate != "" {
		query += ` AND date(timestamp) BETWEEN ? AND ?`
		args = append(args, startDate, endDate)
	} else if startDate != "" {
		query += ` AND date(timestamp) >= ?`
		args = append(args, startDate)
	} else if endDate != "" {
		query += ` AND date(timestamp) <= ?`
		args = append(args, endDate)
	}

	query += ` ORDER BY timestamp ASC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts string
		var productive int
		if err := rows.Scan(&e.ID, &ts, &e.ImagePath, &e.TotalKeys, &e.UniqueKeys,
			&e.EntropyScore, &e.AICategory, &productive, &e.AIConfidence, &e.AIReason); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		e.IsProductive = productive == 1
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetLogsForDate returns all logs for the given date (YYYY-MM-DD).
func (db *DB) GetLogsForDate(date string) ([]LogEntry, error) {
	return db.GetLogsFiltered(0, date, date)
}

// GetTodayLogs returns today's logs.
func (db *DB) GetTodayLogs() ([]LogEntry, error) {
	today := time.Now().Format("2006-01-02")
	return db.GetLogsForDate(today)
}

// GetProductivityStatsFiltered returns aggregated stats filtered by optional user_id and date range.
func (db *DB) GetProductivityStatsFiltered(userID int64, startDate, endDate string) (*ProductivityStats, error) {
	stats := &ProductivityStats{Date: startDate}
	if startDate != "" && endDate != "" && startDate != endDate {
		stats.Date = fmt.Sprintf("%s to %s", startDate, endDate)
	}

	whereClause := ` WHERE 1=1`
	args := []interface{}{}

	if userID > 0 {
		whereClause += ` AND user_id = ?`
		args = append(args, userID)
	}
	if startDate != "" && endDate != "" {
		whereClause += ` AND date(timestamp) BETWEEN ? AND ?`
		args = append(args, startDate, endDate)
	} else if startDate != "" {
		whereClause += ` AND date(timestamp) >= ?`
		args = append(args, startDate)
	} else if endDate != "" {
		whereClause += ` AND date(timestamp) <= ?`
		args = append(args, endDate)
	}

	row := db.conn.QueryRow(`
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN is_productive=1 AND ai_category != '' AND ai_category != 'Unknown' THEN 1 ELSE 0 END) as prod,
			SUM(CASE WHEN is_productive=0 AND ai_category != '' AND ai_category != 'Unknown' THEN 1 ELSE 0 END) as unprod,
			COALESCE(AVG(entropy_score), 0) as avg_entropy
		FROM logs`+whereClause, args...)
	if err := row.Scan(&stats.TotalMinutes, &stats.ProductiveMin,
		&stats.UnproductiveMin, &stats.AvgEntropyScore); err != nil {
		return stats, nil
	}

	// Top category
	catRow := db.conn.QueryRow(`
		SELECT ai_category FROM logs`+whereClause+` AND ai_category != '' AND ai_category != 'Unknown'
		GROUP BY ai_category ORDER BY COUNT(*) DESC LIMIT 1`, args...)
	_ = catRow.Scan(&stats.TopCategory)

	return stats, nil
}

// GetProductivityStats returns aggregated stats for a given date.
func (db *DB) GetProductivityStats(date string) (*ProductivityStats, error) {
	return db.GetProductivityStatsFiltered(0, date, date)
}

// GetUnanalyzedLogs returns all logs that have not yet been successfully analyzed by AI.
func (db *DB) GetUnanalyzedLogs() ([]LogEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, timestamp, image_path, total_keys, unique_keys, entropy_score,
		       ai_category, is_productive, ai_confidence, ai_reason
		FROM logs
		WHERE ai_category = '' OR ai_category = 'Unknown' OR ai_reason LIKE '%No Gemini API key%'
		ORDER BY timestamp ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts string
		var productive int
		if err := rows.Scan(&e.ID, &ts, &e.ImagePath, &e.TotalKeys, &e.UniqueKeys,
			&e.EntropyScore, &e.AICategory, &productive, &e.AIConfidence, &e.AIReason); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		e.IsProductive = productive == 1
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}


