package db

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// LogEntry represents one minute of tracked activity.
type LogEntry struct {
	ID              int64     `json:"id"`
	OrgID           int64     `json:"org_id"`
	UserID          int64     `json:"user_id"`
	Timestamp       time.Time `json:"timestamp"`
	ImagePath       string    `json:"image_path"`
	TotalKeys       int       `json:"total_keys"`
	UniqueKeys      int       `json:"unique_keys"`
	EntropyScore    float64   `json:"entropy_score"`
	AppName         string    `json:"app_name"`
	AppCategory     string    `json:"app_category"`
	WindowTitle     string    `json:"window_title"`
	SessionID       int64     `json:"session_id"`
	SessionTitle    string    `json:"session_title"`
	AICategory      string    `json:"ai_category"`
	IsProductive    bool      `json:"is_productive"`
	ProductiveScore float64   `json:"productive_score"`
	AIConfidence    float64   `json:"ai_confidence"`
	AIReason        string    `json:"ai_reason"`
	SyncStatus      string    `json:"sync_status"`
	RemoteID        int64     `json:"remote_id"`
	SyncedAt        time.Time `json:"synced_at"`
}

// WorkSession represents a continuous block of user activity (e.g. 1:00 PM - 3:00 PM).
type WorkSession struct {
	ID            int64     `json:"id"`
	OrgID         int64     `json:"org_id"`
	UserID        int64     `json:"user_id"`
	Title         string    `json:"title"`
	Summary       string    `json:"summary"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	LogCount      int       `json:"log_count"`
	ProductivePct float64   `json:"productive_pct"`
	TopAppName    string    `json:"top_app_name"`
	TopCategory   string    `json:"top_category"`
	CreatedAt     time.Time `json:"created_at"`
}

// ProductivityStats holds aggregate stats for a day.
type ProductivityStats struct {
	Date            string  `json:"date"`
	TotalMinutes    int     `json:"total_minutes"`
	ProductiveMin   int     `json:"productive_minutes"`
	UnproductiveMin int     `json:"unproductive_minutes"`
	AvgEntropyScore float64 `json:"avg_entropy_score"`
	TopCategory     string  `json:"top_category"`
}

// Organization represents a corporate tenant.
type Organization struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	GeminiAPIKey string    `json:"gemini_api_key,omitempty"`
	GeminiModel  string    `json:"gemini_model,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// User represents a team member.
type User struct {
	ID                   int64     `json:"id"`
	OrgID                int64     `json:"org_id"`
	Email                string    `json:"email"`
	PasswordHash         string    `json:"-"`
	FullName             string    `json:"full_name"`
	Role                 string    `json:"role"`
	PersonalGeminiAPIKey string    `json:"personal_gemini_api_key,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

// Invitation represents an email invite to join an organization.
type Invitation struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Token     string    `json:"token"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// DB wraps Ent ORM driver and database connections.
type DB struct {
	entDriver *entsql.Driver
	rawDB     *sql.DB
	dialect   string
}

// Open initializes database connection using Ent ORM dialect for PostgreSQL (if DATABASE_URL is set) or SQLite.
func Open(dataDir string, dbURLs ...string) (*DB, error) {
	dbURI := ""
	if len(dbURLs) > 0 && dbURLs[0] != "" {
		dbURI = dbURLs[0]
	}
	if dbURI == "" {
		dbURI = os.Getenv("DATABASE_URL")
	}

	var rawDB *sql.DB
	var err error
	drvDialect := dialect.SQLite

	if dbURI != "" {
		drvDialect = dialect.Postgres
		rawDB, err = sql.Open("postgres", dbURI)
		if err != nil {
			return nil, fmt.Errorf("open postgres ent: %w", err)
		}
	} else {
		dbPath := filepath.Join(dataDir, "tracker.db")
		_ = os.MkdirAll(dataDir, 0755)

		rawDB, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
		if err != nil {
			return nil, fmt.Errorf("open sqlite ent: %w", err)
		}
	}

	rawDB.SetMaxOpenConns(25)
	rawDB.SetMaxIdleConns(10)
	rawDB.SetConnMaxLifetime(30 * time.Minute)

	if drvDialect == dialect.SQLite {
		_, _ = rawDB.Exec(`
			PRAGMA cache_size = -64000;
			PRAGMA temp_store = MEMORY;
			PRAGMA mmap_size = 268435456;
		`)
	}

	entDriver := entsql.OpenDB(drvDialect, rawDB)
	db := &DB{entDriver: entDriver, rawDB: rawDB, dialect: drvDialect}

	if err := db.migrate(); err != nil {
		log.Printf("[db] migration note (%s): %v", drvDialect, err)
	}
	return db, nil
}

func (db *DB) migrate() error {
	// Safe column additions for existing databases (run before index creation)
	colMigrations := []string{
		"ALTER TABLE organizations ADD COLUMN gemini_api_key TEXT DEFAULT ''",
		"ALTER TABLE organizations ADD COLUMN gemini_model TEXT DEFAULT ''",
		"ALTER TABLE users ADD COLUMN personal_gemini_api_key TEXT DEFAULT ''",
		"ALTER TABLE logs ADD COLUMN sync_status TEXT DEFAULT 'pending_upload'",
		"ALTER TABLE logs ADD COLUMN remote_id INTEGER DEFAULT 0",
		"ALTER TABLE logs ADD COLUMN synced_at DATETIME",
	}
	for _, alter := range colMigrations {
		_, _ = db.rawDB.Exec(alter)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS organizations (
			id              SERIAL PRIMARY KEY,
			name            TEXT NOT NULL,
			slug            TEXT UNIQUE NOT NULL,
			gemini_api_key  TEXT DEFAULT '',
			gemini_model    TEXT DEFAULT '',
			created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS users (
			id                      SERIAL PRIMARY KEY,
			org_id                  INTEGER NOT NULL REFERENCES organizations(id),
			email                   TEXT UNIQUE NOT NULL,
			password_hash           TEXT NOT NULL,
			full_name               TEXT NOT NULL,
			role                    TEXT CHECK(role IN ('owner', 'admin', 'member')) DEFAULT 'member',
			personal_gemini_api_key TEXT DEFAULT '',
			created_at              TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS invitations (
			id          SERIAL PRIMARY KEY,
			org_id      INTEGER NOT NULL REFERENCES organizations(id),
			email       TEXT NOT NULL,
			role        TEXT CHECK(role IN ('admin', 'member')) DEFAULT 'member',
			token       TEXT UNIQUE NOT NULL,
			status      TEXT CHECK(status IN ('pending', 'accepted', 'expired')) DEFAULT 'pending',
			expires_at  TIMESTAMPTZ NOT NULL,
			created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS logs (
			id               SERIAL PRIMARY KEY,
			org_id           INTEGER DEFAULT 1,
			user_id          INTEGER DEFAULT 1,
			timestamp        TIMESTAMPTZ NOT NULL,
			image_path       TEXT,
			total_keys       INTEGER DEFAULT 0,
			unique_keys      INTEGER DEFAULT 0,
			entropy_score    DOUBLE PRECISION DEFAULT 0,
			app_name         TEXT DEFAULT '',
			app_category     TEXT DEFAULT '',
			window_title     TEXT DEFAULT '',
			session_id       INTEGER DEFAULT 0,
			session_title    TEXT DEFAULT '',
			ai_category      TEXT DEFAULT '',
			is_productive    INTEGER DEFAULT 0,
			productive_score DOUBLE PRECISION DEFAULT 0,
			ai_confidence    DOUBLE PRECISION DEFAULT 0,
			ai_reason        TEXT DEFAULT '',
			sync_status      TEXT DEFAULT 'pending_upload',
			remote_id        INTEGER DEFAULT 0,
			synced_at        TIMESTAMPTZ
		);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_user_ts ON logs(user_id, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_org_ts ON logs(org_id, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_unanalyzed ON logs(ai_category, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_sync_status ON logs(sync_status);`,
		`CREATE TABLE IF NOT EXISTS work_sessions (
			id             SERIAL PRIMARY KEY,
			org_id         INTEGER DEFAULT 1,
			user_id        INTEGER DEFAULT 1,
			title          TEXT NOT NULL,
			summary        TEXT DEFAULT '',
			start_time     TIMESTAMPTZ NOT NULL,
			end_time       TIMESTAMPTZ NOT NULL,
			log_count      INTEGER DEFAULT 0,
			productive_pct DOUBLE PRECISION DEFAULT 0,
			top_app_name   TEXT DEFAULT '',
			top_category   TEXT DEFAULT '',
			created_at     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_work_sessions_user_time ON work_sessions(user_id, start_time, end_time);`,
	}

	if db.dialect == dialect.SQLite {
		stmts = []string{
			`CREATE TABLE IF NOT EXISTS organizations (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				name            TEXT NOT NULL,
				slug            TEXT UNIQUE NOT NULL,
				gemini_api_key  TEXT DEFAULT '',
				gemini_model    TEXT DEFAULT '',
				created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
			);`,
			`CREATE TABLE IF NOT EXISTS users (
				id                      INTEGER PRIMARY KEY AUTOINCREMENT,
				org_id                  INTEGER NOT NULL,
				email                   TEXT UNIQUE NOT NULL,
				password_hash           TEXT NOT NULL,
				full_name               TEXT NOT NULL,
				role                    TEXT CHECK(role IN ('owner', 'admin', 'member')) DEFAULT 'member',
				personal_gemini_api_key TEXT DEFAULT '',
				created_at              DATETIME DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(org_id) REFERENCES organizations(id)
			);`,
			`CREATE TABLE IF NOT EXISTS invitations (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				org_id      INTEGER NOT NULL,
				email       TEXT NOT NULL,
				role        TEXT CHECK(role IN ('admin', 'member')) DEFAULT 'member',
				token       TEXT UNIQUE NOT NULL,
				status      TEXT CHECK(status IN ('pending', 'accepted', 'expired')) DEFAULT 'pending',
				expires_at  DATETIME NOT NULL,
				created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(org_id) REFERENCES organizations(id)
			);`,
			`CREATE TABLE IF NOT EXISTS logs (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				org_id           INTEGER DEFAULT 1,
				user_id          INTEGER DEFAULT 1,
				timestamp        DATETIME NOT NULL,
				image_path       TEXT,
				total_keys       INTEGER DEFAULT 0,
				unique_keys      INTEGER DEFAULT 0,
				entropy_score    REAL DEFAULT 0,
				app_name         TEXT DEFAULT '',
				app_category     TEXT DEFAULT '',
				window_title     TEXT DEFAULT '',
				session_id       INTEGER DEFAULT 0,
				session_title    TEXT DEFAULT '',
				ai_category      TEXT DEFAULT '',
				is_productive    INTEGER DEFAULT 0,
				productive_score REAL DEFAULT 0,
				ai_confidence    REAL DEFAULT 0,
				ai_reason        TEXT DEFAULT '',
				sync_status      TEXT DEFAULT 'pending_upload',
				remote_id        INTEGER DEFAULT 0,
				synced_at        DATETIME
			);`,
			`CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);`,
			`CREATE INDEX IF NOT EXISTS idx_logs_user_ts ON logs(user_id, timestamp);`,
			`CREATE INDEX IF NOT EXISTS idx_logs_org_ts ON logs(org_id, timestamp);`,
			`CREATE INDEX IF NOT EXISTS idx_logs_unanalyzed ON logs(ai_category, timestamp);`,
			`CREATE INDEX IF NOT EXISTS idx_logs_sync_status ON logs(sync_status);`,
			`CREATE TABLE IF NOT EXISTS work_sessions (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				org_id         INTEGER DEFAULT 1,
				user_id        INTEGER DEFAULT 1,
				title          TEXT NOT NULL,
				summary        TEXT DEFAULT '',
				start_time     DATETIME NOT NULL,
				end_time       DATETIME NOT NULL,
				log_count      INTEGER DEFAULT 0,
				productive_pct REAL DEFAULT 0,
				top_app_name   TEXT DEFAULT '',
				top_category   TEXT DEFAULT '',
				created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
			);`,
			`CREATE INDEX IF NOT EXISTS idx_work_sessions_user_time ON work_sessions(user_id, start_time, end_time);`,
		}
	}

	for _, stmt := range stmts {
		if _, err := db.rawDB.Exec(stmt); err != nil {
			log.Printf("[db] migration statement note: %v", err)
		}
	}

	// Seed default org
	query := entsql.Dialect(db.dialect).Select(entsql.Count("*")).From(entsql.Table("organizations"))
	selector, args := query.Query()
	var orgCount int
	_ = db.rawDB.QueryRow(selector, args...).Scan(&orgCount)
	if orgCount == 0 {
		adminHash := HashPassword("admin123")
		memberHash := HashPassword("user123")
		_, _ = db.CreateOrganization("Default Beta Org", "default-beta")
		_, _ = db.CreateUser(1, "admin@company.com", adminHash, "Beta Admin", "owner")
		_, _ = db.CreateUser(1, "user@company.com", memberHash, "Test Team Member", "member")
	}

	return nil
}

// HashPassword hashes a user password consistently using SHA-256 with a salt.
func HashPassword(password string) string {
	h := sha256.Sum256([]byte("get-hike-salt-" + password))
	return fmt.Sprintf("%x", h)
}

// CreateOrganization creates a new corporate entity using Ent SQL Builder.
func (db *DB) CreateOrganization(name, slug string) (*Organization, error) {
	builder := entsql.Dialect(db.dialect).Insert("organizations").Columns("name", "slug").Values(name, slug)
	if db.dialect == dialect.Postgres {
		builder.Returning("id")
		query, args := builder.Query()
		var id int64
		if err := db.rawDB.QueryRow(query, args...).Scan(&id); err != nil {
			return nil, fmt.Errorf("create org: %w", err)
		}
		return &Organization{ID: id, Name: name, Slug: slug, CreatedAt: time.Now()}, nil
	}

	query, args := builder.Query()
	res, err := db.rawDB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("create org: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Organization{ID: id, Name: name, Slug: slug, CreatedAt: time.Now()}, nil
}

// GetOrganization returns an org by ID using Ent SQL Builder.
func (db *DB) GetOrganization(id int64) (*Organization, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "name", "slug", "created_at").From(entsql.Table("organizations")).Where(entsql.EQ("id", id))
	query, args := builder.Query()
	var o Organization
	var ts interface{}
	if err := db.rawDB.QueryRow(query, args...).Scan(&o.ID, &o.Name, &o.Slug, &ts); err != nil {
		return nil, err
	}
	o.CreatedAt = parseFlexibleTime(ts)
	return &o, nil
}

// GetOrganizationBySlug returns an org by slug using Ent SQL Builder.
func (db *DB) GetOrganizationBySlug(slug string) (*Organization, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "name", "slug", "created_at").From(entsql.Table("organizations")).Where(entsql.EQ("slug", slug))
	query, args := builder.Query()
	var o Organization
	var ts interface{}
	if err := db.rawDB.QueryRow(query, args...).Scan(&o.ID, &o.Name, &o.Slug, &ts); err != nil {
		return nil, err
	}
	o.CreatedAt = parseFlexibleTime(ts)
	return &o, nil
}

// CreateUser creates a team member account using Ent SQL Builder.
func (db *DB) CreateUser(orgID int64, email, passwordHash, fullName, role string) (*User, error) {
	builder := entsql.Dialect(db.dialect).Insert("users").Columns("org_id", "email", "password_hash", "full_name", "role").Values(orgID, email, passwordHash, fullName, role)
	if db.dialect == dialect.Postgres {
		builder.Returning("id")
		query, args := builder.Query()
		var id int64
		if err := db.rawDB.QueryRow(query, args...).Scan(&id); err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return &User{ID: id, OrgID: orgID, Email: email, PasswordHash: passwordHash, FullName: fullName, Role: role, CreatedAt: time.Now()}, nil
	}

	query, args := builder.Query()
	res, err := db.rawDB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, OrgID: orgID, Email: email, PasswordHash: passwordHash, FullName: fullName, Role: role, CreatedAt: time.Now()}, nil
}

// GetUserByEmail finds a user by email using Ent SQL Builder.
func (db *DB) GetUserByEmail(email string) (*User, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "org_id", "email", "password_hash", "full_name", "role", "created_at").From(entsql.Table("users")).Where(entsql.EQ("email", email))
	query, args := builder.Query()
	var u User
	var ts interface{}
	if err := db.rawDB.QueryRow(query, args...).Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role, &ts); err != nil {
		return nil, err
	}
	u.CreatedAt = parseFlexibleTime(ts)
	return &u, nil
}

// GetUserByID finds a user by ID using Ent SQL Builder.
func (db *DB) GetUserByID(id int64) (*User, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "org_id", "email", "password_hash", "full_name", "role", "created_at").From(entsql.Table("users")).Where(entsql.EQ("id", id))
	query, args := builder.Query()
	var u User
	var ts interface{}
	if err := db.rawDB.QueryRow(query, args...).Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role, &ts); err != nil {
		return nil, err
	}
	u.CreatedAt = parseFlexibleTime(ts)
	return &u, nil
}

// CreateInvitation creates an email invite record using Ent SQL Builder.
func (db *DB) CreateInvitation(orgID int64, email, role, token string, expiresAt time.Time) (*Invitation, error) {
	checkQ := entsql.Dialect(db.dialect).Select(entsql.Count("*")).From(entsql.Table("users")).Where(entsql.And(entsql.EQ("org_id", orgID), entsql.EQ("email", email)))
	cQuery, cArgs := checkQ.Query()
	var count int
	_ = db.rawDB.QueryRow(cQuery, cArgs...).Scan(&count)
	if count > 0 {
		return nil, fmt.Errorf("user %s is already a member of this organization", email)
	}

	expQ := entsql.Dialect(db.dialect).Update("invitations").Set("status", "expired").Where(entsql.And(entsql.EQ("org_id", orgID), entsql.EQ("email", email), entsql.EQ("status", "pending")))
	eQuery, eArgs := expQ.Query()
	_, _ = db.rawDB.Exec(eQuery, eArgs...)

	builder := entsql.Dialect(db.dialect).Insert("invitations").Columns("org_id", "email", "role", "token", "status", "expires_at").Values(orgID, email, role, token, "pending", expiresAt.UTC().Format(time.RFC3339))
	if db.dialect == dialect.Postgres {
		builder.Returning("id")
		query, args := builder.Query()
		var id int64
		if err := db.rawDB.QueryRow(query, args...).Scan(&id); err != nil {
			return nil, fmt.Errorf("create invitation: %w", err)
		}
		return &Invitation{ID: id, OrgID: orgID, Email: email, Role: role, Token: token, Status: "pending", ExpiresAt: expiresAt, CreatedAt: time.Now()}, nil
	}

	query, args := builder.Query()
	res, err := db.rawDB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("create invitation: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Invitation{ID: id, OrgID: orgID, Email: email, Role: role, Token: token, Status: "pending", ExpiresAt: expiresAt, CreatedAt: time.Now()}, nil
}

// GetInvitationByToken returns an invitation by its token using Ent SQL Builder.
func (db *DB) GetInvitationByToken(token string) (*Invitation, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "org_id", "email", "role", "token", "status", "expires_at", "created_at").From(entsql.Table("invitations")).Where(entsql.EQ("token", token))
	query, args := builder.Query()
	var inv Invitation
	var expTs, crTs interface{}
	if err := db.rawDB.QueryRow(query, args...).Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.Token, &inv.Status, &expTs, &crTs); err != nil {
		return nil, err
	}
	inv.ExpiresAt = parseFlexibleTime(expTs)
	inv.CreatedAt = parseFlexibleTime(crTs)

	if inv.Status == "pending" && time.Now().After(inv.ExpiresAt) {
		inv.Status = "expired"
		upQ := entsql.Dialect(db.dialect).Update("invitations").Set("status", "expired").Where(entsql.EQ("id", inv.ID))
		uQuery, uArgs := upQ.Query()
		_, _ = db.rawDB.Exec(uQuery, uArgs...)
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

	upQ := entsql.Dialect(db.dialect).Update("invitations").Set("status", "accepted").Where(entsql.EQ("id", inv.ID))
	uQuery, uArgs := upQ.Query()
	_, _ = db.rawDB.Exec(uQuery, uArgs...)
	return user, nil
}

// GetOrgMembers lists all members of an organization using Ent SQL Builder.
func (db *DB) GetOrgMembers(orgID int64) ([]User, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "org_id", "email", "full_name", "role", "created_at").From(entsql.Table("users")).Where(entsql.EQ("org_id", orgID)).OrderBy(entsql.Asc("id"))
	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var ts interface{}
		if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.FullName, &u.Role, &ts); err != nil {
			return nil, err
		}
		u.CreatedAt = parseFlexibleTime(ts)
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetPendingInvitations lists all active pending invites using Ent SQL Builder.
func (db *DB) GetPendingInvitations(orgID int64) ([]Invitation, error) {
	builder := entsql.Dialect(db.dialect).Select("id", "org_id", "email", "role", "token", "status", "expires_at", "created_at").From(entsql.Table("invitations")).Where(entsql.And(entsql.EQ("org_id", orgID), entsql.EQ("status", "pending"))).OrderBy(entsql.Desc("id"))
	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invs []Invitation
	for rows.Next() {
		var inv Invitation
		var expTs, crTs interface{}
		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.Token, &inv.Status, &expTs, &crTs); err != nil {
			return nil, err
		}
		inv.ExpiresAt = parseFlexibleTime(expTs)
		inv.CreatedAt = parseFlexibleTime(crTs)

		if time.Now().After(inv.ExpiresAt) {
			inv.Status = "expired"
			upQ := entsql.Dialect(db.dialect).Update("invitations").Set("status", "expired").Where(entsql.EQ("id", inv.ID))
			uQuery, uArgs := upQ.Query()
			_, _ = db.rawDB.Exec(uQuery, uArgs...)
			continue
		}
		invs = append(invs, inv)
	}
	return invs, rows.Err()
}

// InsertLog stores a new log entry using Ent SQL Builder.
func (db *DB) InsertLog(e *LogEntry) (int64, error) {
	builder := entsql.Dialect(db.dialect).Insert("logs").Columns("timestamp", "image_path", "total_keys", "unique_keys", "entropy_score").Values(e.Timestamp.UTC().Format(time.RFC3339), e.ImagePath, e.TotalKeys, e.UniqueKeys, e.EntropyScore)
	if db.dialect == dialect.Postgres {
		builder.Returning("id")
		query, args := builder.Query()
		var id int64
		err := db.rawDB.QueryRow(query, args...).Scan(&id)
		return id, err
	}

	query, args := builder.Query()
	res, err := db.rawDB.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateAIResult patches AI fields for a given log ID.
func (db *DB) UpdateAIResult(id int64, category string, productive bool, confidence float64, reason string) error {
	score := 0.0
	if productive {
		score = 100.0
	}
	return db.UpdateLogAnalysis(id, category, "", "", "", 0, "", productive, score, confidence, reason)
}

// UpdateLogAnalysis patches AI fields using Ent SQL Builder.
func (db *DB) UpdateLogAnalysis(id int64, category, appName, appCategory, windowTitle string, sessionID int64, sessionTitle string, productive bool, productiveScore float64, confidence float64, reason string) error {
	productiveInt := 0
	if productive || productiveScore >= 50 {
		productiveInt = 1
	}
	if productiveScore == 0 && productive {
		productiveScore = 100.0
	}

	builder := entsql.Dialect(db.dialect).Update("logs").
		Set("ai_category", category).
		Set("app_name", appName).
		Set("app_category", appCategory).
		Set("window_title", windowTitle).
		Set("session_id", sessionID).
		Set("session_title", sessionTitle).
		Set("is_productive", productiveInt).
		Set("productive_score", productiveScore).
		Set("ai_confidence", confidence).
		Set("ai_reason", reason).
		Where(entsql.EQ("id", id))

	query, args := builder.Query()
	_, err := db.rawDB.Exec(query, args...)
	return err
}

// GetLogsFiltered returns logs using Ent SQL Builder with cross-dialect date predicate.
func (db *DB) GetLogsFiltered(userID, orgID int64, startDate, endDate string) ([]LogEntry, error) {
	builder := entsql.Dialect(db.dialect).Select(
		"id", "timestamp", "image_path", "total_keys", "unique_keys", "entropy_score",
		"app_name", "app_category", "window_title", "session_id", "session_title",
		"ai_category", "is_productive", "productive_score", "ai_confidence", "ai_reason",
	).From(entsql.Table("logs"))

	predicates := []*entsql.Predicate{}
	if userID > 0 {
		predicates = append(predicates, entsql.EQ("user_id", userID))
	}
	if orgID > 0 {
		predicates = append(predicates, entsql.EQ("org_id", orgID))
	}

	if startDate != "" && endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date BETWEEN ").Arg(startDate).WriteString("::date AND ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) BETWEEN ").Arg(startDate).WriteString(" AND ").Arg(endDate)
			}))
		}
	} else if startDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date >= ").Arg(startDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) >= ").Arg(startDate)
			}))
		}
	} else if endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date <= ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) <= ").Arg(endDate)
			}))
		}
	}

	if len(predicates) > 0 {
		builder.Where(entsql.And(predicates...))
	}
	builder.OrderBy(entsql.Asc("timestamp"))

	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts interface{}
		var productive int
		if err := rows.Scan(&e.ID, &ts, &e.ImagePath, &e.TotalKeys, &e.UniqueKeys, &e.EntropyScore,
			&e.AppName, &e.AppCategory, &e.WindowTitle, &e.SessionID, &e.SessionTitle,
			&e.AICategory, &productive, &e.ProductiveScore, &e.AIConfidence, &e.AIReason); err != nil {
			return nil, err
		}
		e.Timestamp = parseFlexibleTime(ts)
		e.IsProductive = productive == 1 || e.ProductiveScore >= 50
		if e.ProductiveScore == 0 && e.IsProductive {
			e.ProductiveScore = 100
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// PaginatedLogResponse holds paginated entries and metadata.
type PaginatedLogResponse struct {
	Logs       []LogEntry `json:"logs"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	TotalPages int        `json:"total_pages"`
}

// GetLogsFilteredPaginated returns logs using Ent SQL Builder with pagination.
func (db *DB) GetLogsFilteredPaginated(userID, orgID int64, startDate, endDate string, page, limit int) (*PaginatedLogResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}

	offset := (page - 1) * limit

	predicates := []*entsql.Predicate{}
	if userID > 0 {
		predicates = append(predicates, entsql.EQ("user_id", userID))
	}
	if orgID > 0 {
		predicates = append(predicates, entsql.EQ("org_id", orgID))
	}
	if startDate != "" && endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date BETWEEN ").Arg(startDate).WriteString("::date AND ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) BETWEEN ").Arg(startDate).WriteString(" AND ").Arg(endDate)
			}))
		}
	} else if startDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date >= ").Arg(startDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) >= ").Arg(startDate)
			}))
		}
	} else if endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date <= ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) <= ").Arg(endDate)
			}))
		}
	}

	countBuilder := entsql.Dialect(db.dialect).Select(entsql.Count("*")).From(entsql.Table("logs"))
	if len(predicates) > 0 {
		countBuilder.Where(entsql.And(predicates...))
	}

	cQuery, cArgs := countBuilder.Query()
	var total int
	if err := db.rawDB.QueryRow(cQuery, cArgs...).Scan(&total); err != nil {
		return nil, err
	}

	builder := entsql.Dialect(db.dialect).Select(
		"id", "timestamp", "image_path", "total_keys", "unique_keys", "entropy_score",
		"app_name", "app_category", "window_title", "session_id", "session_title",
		"ai_category", "is_productive", "productive_score", "ai_confidence", "ai_reason",
	).From(entsql.Table("logs"))

	if len(predicates) > 0 {
		builder.Where(entsql.And(predicates...))
	}
	builder.OrderBy(entsql.Asc("timestamp")).Offset(offset).Limit(limit)

	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts interface{}
		var productive int
		if err := rows.Scan(&e.ID, &ts, &e.ImagePath, &e.TotalKeys, &e.UniqueKeys, &e.EntropyScore,
			&e.AppName, &e.AppCategory, &e.WindowTitle, &e.SessionID, &e.SessionTitle,
			&e.AICategory, &productive, &e.ProductiveScore, &e.AIConfidence, &e.AIReason); err != nil {
			return nil, err
		}
		e.Timestamp = parseFlexibleTime(ts)
		e.IsProductive = productive == 1 || e.ProductiveScore >= 50
		if e.ProductiveScore == 0 && e.IsProductive {
			e.ProductiveScore = 100
		}
		entries = append(entries, e)
	}

	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	return &PaginatedLogResponse{
		Logs:       entries,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}, rows.Err()
}

// GetLogsForDate returns all logs for the given date (YYYY-MM-DD).
func (db *DB) GetLogsForDate(date string) ([]LogEntry, error) {
	return db.GetLogsFiltered(0, 0, date, date)
}

// GetTodayLogs returns today's logs.
func (db *DB) GetTodayLogs() ([]LogEntry, error) {
	today := time.Now().Format("2006-01-02")
	return db.GetLogsForDate(today)
}

// SaveWorkSessions batch inserts work session summaries using Ent SQL Builder.
func (db *DB) SaveWorkSessions(sessions []WorkSession) error {
	for _, s := range sessions {
		builder := entsql.Dialect(db.dialect).Insert("work_sessions").
			Columns("org_id", "user_id", "title", "summary", "start_time", "end_time", "log_count", "productive_pct", "top_app_name", "top_category").
			Values(s.OrgID, s.UserID, s.Title, s.Summary, s.StartTime, s.EndTime, s.LogCount, s.ProductivePct, s.TopAppName, s.TopCategory)
		query, args := builder.Query()
		_, _ = db.rawDB.Exec(query, args...)
	}
	return nil
}

// GetWorkSessionsFiltered returns work sessions using Ent SQL Builder.
func (db *DB) GetWorkSessionsFiltered(userID, orgID int64, startDate, endDate string) ([]WorkSession, error) {
	builder := entsql.Dialect(db.dialect).Select(
		"id", "org_id", "user_id", "title", "summary", "start_time", "end_time", "log_count", "productive_pct", "top_app_name", "top_category", "created_at",
	).From(entsql.Table("work_sessions"))

	predicates := []*entsql.Predicate{}
	if userID > 0 {
		predicates = append(predicates, entsql.EQ("user_id", userID))
	}
	if orgID > 0 {
		predicates = append(predicates, entsql.EQ("org_id", orgID))
	}
	if startDate != "" && endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("start_time::date BETWEEN ").Arg(startDate).WriteString("::date AND ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("date(start_time) BETWEEN ").Arg(startDate).WriteString(" AND ").Arg(endDate)
			}))
		}
	} else if startDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("start_time::date >= ").Arg(startDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("date(start_time) >= ").Arg(startDate)
			}))
		}
	}

	if len(predicates) > 0 {
		builder.Where(entsql.And(predicates...))
	}
	builder.OrderBy(entsql.Desc("start_time"))

	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []WorkSession
	for rows.Next() {
		var s WorkSession
		var startTs, endTs, crTs interface{}
		if err := rows.Scan(&s.ID, &s.OrgID, &s.UserID, &s.Title, &s.Summary, &startTs, &endTs, &s.LogCount, &s.ProductivePct, &s.TopAppName, &s.TopCategory, &crTs); err != nil {
			return nil, err
		}
		s.StartTime = parseFlexibleTime(startTs)
		s.EndTime = parseFlexibleTime(endTs)
		s.CreatedAt = parseFlexibleTime(crTs)
		sessions = append(sessions, s)
	}

	if len(sessions) == 0 {
		logs, err := db.GetLogsFiltered(userID, orgID, startDate, endDate)
		if err == nil && len(logs) > 0 {
			sessions = ComputeWorkSessionsFromLogs(logs)
		}
	}

	return sessions, nil
}

// GetWorkSessionsForDate aggregates activity captures for a date into high-level work sessions.
func (db *DB) GetWorkSessionsForDate(date string) ([]WorkSession, error) {
	return db.GetWorkSessionsFiltered(0, 0, date, date)
}

// ComputeWorkSessionsFromLogs clusters raw log entries into continuous work sessions.
func ComputeWorkSessionsFromLogs(logs []LogEntry) []WorkSession {
	if len(logs) == 0 {
		return nil
	}

	var sessions []WorkSession
	var currentCluster []LogEntry

	for _, l := range logs {
		if len(currentCluster) == 0 {
			currentCluster = append(currentCluster, l)
			continue
		}

		prev := currentCluster[len(currentCluster)-1]
		gap := l.Timestamp.Sub(prev.Timestamp)

		if gap <= 15*time.Minute {
			currentCluster = append(currentCluster, l)
		} else {
			sessions = append(sessions, buildWorkSession(currentCluster))
			currentCluster = []LogEntry{l}
		}
	}

	if len(currentCluster) > 0 {
		sessions = append(sessions, buildWorkSession(currentCluster))
	}

	return sessions
}

func buildWorkSession(cluster []LogEntry) WorkSession {
	startTime := cluster[0].Timestamp
	endTime := cluster[len(cluster)-1].Timestamp
	logCount := len(cluster)

	productiveCount := 0
	appCounts := make(map[string]int)
	catCounts := make(map[string]int)

	for _, l := range cluster {
		if l.IsProductive {
			productiveCount++
		}
		if l.AppName != "" {
			appCounts[l.AppName]++
		}
		if l.AICategory != "" {
			catCounts[l.AICategory]++
		}
	}

	topApp := getTopKey(appCounts, "Active Work")
	topCat := getTopKey(catCounts, "General Activity")
	productivePct := float64(productiveCount) / float64(logCount) * 100

	title := fmt.Sprintf("%s - %s • %s (%s)",
		startTime.Format("15:04"),
		endTime.Format("15:04"),
		topApp,
		topCat,
	)

	summary := fmt.Sprintf("%d captures recorded. %d%% productive rate focused on %s.",
		logCount, int(productivePct), topApp)

	return WorkSession{
		ID:            startTime.Unix(),
		OrgID:         1,
		UserID:        1,
		Title:         title,
		Summary:       summary,
		StartTime:     startTime,
		EndTime:       endTime,
		LogCount:      logCount,
		ProductivePct: productivePct,
		TopAppName:    topApp,
		TopCategory:   topCat,
		CreatedAt:     startTime,
	}
}

func getTopKey(m map[string]int, fallback string) string {
	topK := fallback
	maxC := 0
	for k, v := range m {
		if v > maxC {
			maxC = v
			topK = k
		}
	}
	return topK
}

// GetProductivityStatsFiltered returns aggregated stats using Ent SQL Builder.
func (db *DB) GetProductivityStatsFiltered(userID, orgID int64, startDate, endDate string) (*ProductivityStats, error) {
	stats := &ProductivityStats{Date: startDate}
	if startDate != "" && endDate != "" && startDate != endDate {
		stats.Date = fmt.Sprintf("%s to %s", startDate, endDate)
	}

	predicates := []*entsql.Predicate{}
	if userID > 0 {
		predicates = append(predicates, entsql.EQ("user_id", userID))
	}
	if orgID > 0 {
		predicates = append(predicates, entsql.EQ("org_id", orgID))
	}
	if startDate != "" && endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date BETWEEN ").Arg(startDate).WriteString("::date AND ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) BETWEEN ").Arg(startDate).WriteString(" AND ").Arg(endDate)
			}))
		}
	} else if startDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date >= ").Arg(startDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) >= ").Arg(startDate)
			}))
		}
	} else if endDate != "" {
		if db.dialect == dialect.Postgres {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("timestamp::date <= ").Arg(endDate).WriteString("::date")
			}))
		} else {
			predicates = append(predicates, entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(timestamp, 1, 10) <= ").Arg(endDate)
			}))
		}
	}

	builder := entsql.Dialect(db.dialect).Select(
		"COUNT(*)",
		"COALESCE(SUM(CASE WHEN is_productive=1 AND ai_category != '' AND ai_category != 'Unknown' THEN 1 ELSE 0 END), 0)",
		"COALESCE(SUM(CASE WHEN is_productive=0 AND ai_category != '' AND ai_category != 'Unknown' THEN 1 ELSE 0 END), 0)",
		"COALESCE(AVG(entropy_score), 0)",
	).From(entsql.Table("logs"))

	if len(predicates) > 0 {
		builder.Where(entsql.And(predicates...))
	}

	query, args := builder.Query()
	row := db.rawDB.QueryRow(query, args...)

	var total, prod, unprod sql.NullInt64
	var avgEntropy sql.NullFloat64
	if err := row.Scan(&total, &prod, &unprod, &avgEntropy); err == nil {
		stats.TotalMinutes = int(total.Int64)
		stats.ProductiveMin = int(prod.Int64)
		stats.UnproductiveMin = int(unprod.Int64)
		stats.AvgEntropyScore = avgEntropy.Float64
	}

	// Top category query
	catBuilder := entsql.Dialect(db.dialect).Select("ai_category").From(entsql.Table("logs")).
		Where(entsql.And(entsql.NEQ("ai_category", ""), entsql.NEQ("ai_category", "Unknown")))
	if len(predicates) > 0 {
		catBuilder.Where(entsql.And(predicates...))
	}
	catBuilder.GroupBy("ai_category").OrderBy(entsql.Desc(entsql.Count("*"))).Limit(1)
	cQuery, cArgs := catBuilder.Query()
	_ = db.rawDB.QueryRow(cQuery, cArgs...).Scan(&stats.TopCategory)

	return stats, nil
}

// GetProductivityStats returns aggregated stats for a given date.
func (db *DB) GetProductivityStats(date string) (*ProductivityStats, error) {
	return db.GetProductivityStatsFiltered(0, 0, date, date)
}

// GetUnanalyzedLogs returns all logs that have not yet been successfully analyzed by AI using Ent SQL Builder.
func (db *DB) GetUnanalyzedLogs() ([]LogEntry, error) {
	builder := entsql.Dialect(db.dialect).Select(
		"id", "timestamp", "image_path", "total_keys", "unique_keys", "entropy_score",
		"app_name", "app_category", "window_title", "session_id", "session_title",
		"ai_category", "is_productive", "productive_score", "ai_confidence", "ai_reason",
	).From(entsql.Table("logs")).Where(
		entsql.Or(
			entsql.EQ("ai_category", ""),
			entsql.EQ("ai_category", "Unknown"),
			entsql.Like("ai_reason", "%No Gemini API key%"),
		),
	).OrderBy(entsql.Asc("timestamp"))

	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts interface{}
		var productive int
		if err := rows.Scan(&e.ID, &ts, &e.ImagePath, &e.TotalKeys, &e.UniqueKeys, &e.EntropyScore,
			&e.AppName, &e.AppCategory, &e.WindowTitle, &e.SessionID, &e.SessionTitle,
			&e.AICategory, &productive, &e.ProductiveScore, &e.AIConfidence, &e.AIReason); err != nil {
			return nil, err
		}
		e.Timestamp = parseFlexibleTime(ts)
		e.IsProductive = productive == 1 || e.ProductiveScore >= 50
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func parseFlexibleTime(v interface{}) time.Time {
	if v == nil {
		return time.Now()
	}
	if t, ok := v.(time.Time); ok {
		return t
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02 15:04:05-07", s); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z07:00", s); err == nil {
			return t
		}
	}
	return time.Now()
}

// GetPendingUploadLogs retrieves local logs waiting to be pushed to backend.
func (db *DB) GetPendingUploadLogs(limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	builder := entsql.Dialect(db.dialect).Select(
		"id", "org_id", "user_id", "timestamp", "image_path",
		"total_keys", "unique_keys", "entropy_score",
		"app_name", "app_category", "window_title",
		"session_id", "session_title", "ai_category",
		"is_productive", "productive_score", "ai_confidence", "ai_reason",
	).From(entsql.Table("logs")).Where(
		entsql.Or(
			entsql.EQ("sync_status", "pending_upload"),
			entsql.EQ("sync_status", ""),
		),
	).OrderBy(entsql.Asc("timestamp")).Limit(limit)

	query, args := builder.Query()
	rows, err := db.rawDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts interface{}
		var productive int
		if err := rows.Scan(&e.ID, &e.OrgID, &e.UserID, &ts, &e.ImagePath,
			&e.TotalKeys, &e.UniqueKeys, &e.EntropyScore,
			&e.AppName, &e.AppCategory, &e.WindowTitle,
			&e.SessionID, &e.SessionTitle, &e.AICategory,
			&productive, &e.ProductiveScore, &e.AIConfidence, &e.AIReason); err != nil {
			return nil, err
		}
		e.Timestamp = parseFlexibleTime(ts)
		e.IsProductive = productive == 1 || e.ProductiveScore >= 50
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// MarkLogsSynced marks local log entries as synced with remote IDs.
func (db *DB) MarkLogsSynced(localIDs []int64, remoteIDs []int64) error {
	if len(localIDs) == 0 {
		return nil
	}
	now := time.Now()
	for i, localID := range localIDs {
		var remoteID int64
		if i < len(remoteIDs) {
			remoteID = remoteIDs[i]
		}
		builder := entsql.Dialect(db.dialect).Update("logs").
			Set("sync_status", "synced").
			Set("remote_id", remoteID).
			Set("synced_at", now).
			Where(entsql.EQ("id", localID))
		query, args := builder.Query()
		_, _ = db.rawDB.Exec(query, args...)
	}
	return nil
}

// SetOrgGeminiConfig updates the Gemini API key and model for an organization.
func (db *DB) SetOrgGeminiConfig(orgID int64, apiKey, model string) error {
	builder := entsql.Dialect(db.dialect).Update("organizations").
		Set("gemini_api_key", apiKey).
		Set("gemini_model", model).
		Where(entsql.EQ("id", orgID))
	query, args := builder.Query()
	_, err := db.rawDB.Exec(query, args...)
	return err
}

// SetUserPersonalKey updates the personal Gemini API key for a user.
func (db *DB) SetUserPersonalKey(userID int64, apiKey string) error {
	builder := entsql.Dialect(db.dialect).Update("users").
		Set("personal_gemini_api_key", apiKey).
		Where(entsql.EQ("id", userID))
	query, args := builder.Query()
	_, err := db.rawDB.Exec(query, args...)
	return err
}

// ResolveEffectiveGeminiKey determines the Gemini API key and model based on hierarchy:
// 1. Org Admin Key in DB (for Org members)
// 2. User Personal Key in DB/Config (for Solo Non-Org users)
// 3. Global System Key (fallback)
func (db *DB) ResolveEffectiveGeminiKey(userID, orgID int64, defaultKey, defaultModel string) (apiKey string, model string, source string, err error) {
	if defaultModel == "" {
		defaultModel = "models/gemma-4-31b-it"
	}

	// 1. Check if user is part of an Organization with an Admin-configured key
	if orgID > 0 {
		orgBuilder := entsql.Dialect(db.dialect).Select("gemini_api_key", "gemini_model").
			From(entsql.Table("organizations")).Where(entsql.EQ("id", orgID))
		query, args := orgBuilder.Query()
		var orgKey, orgModel sql.NullString
		if err := db.rawDB.QueryRow(query, args...).Scan(&orgKey, &orgModel); err == nil {
			if orgKey.Valid && strings.TrimSpace(orgKey.String) != "" {
				m := defaultModel
				if orgModel.Valid && strings.TrimSpace(orgModel.String) != "" {
					m = orgModel.String
				}
				return strings.TrimSpace(orgKey.String), m, "org_admin", nil
			}
		}
	}

	// 2. Check if Solo user has set a Personal API Key
	if userID > 0 {
		userBuilder := entsql.Dialect(db.dialect).Select("personal_gemini_api_key").
			From(entsql.Table("users")).Where(entsql.EQ("id", userID))
		query, args := userBuilder.Query()
		var userKey sql.NullString
		if err := db.rawDB.QueryRow(query, args...).Scan(&userKey); err == nil {
			if userKey.Valid && strings.TrimSpace(userKey.String) != "" {
				return strings.TrimSpace(userKey.String), defaultModel, "user_personal", nil
			}
		}
	}

	// 3. Fallback to Global System Key
	if strings.TrimSpace(defaultKey) != "" {
		return strings.TrimSpace(defaultKey), defaultModel, "global_system", nil
	}

	return "", defaultModel, "none", nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.entDriver != nil {
		return db.entDriver.Close()
	}
	return db.rawDB.Close()
}
