package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"dbird/internal/store"
)

// ErrNotConnected is returned when an operation needs an open connection.
var ErrNotConnected = errors.New("not connected")

// ErrBusy is returned when an editor session is already running a query.
var ErrBusy = errors.New("a query is already running in this tab")

// Manager owns the open connection pools and the per-editor sessions.
//
// Each editor tab gets its own dedicated *sql.Conn so session state (SET,
// USE, BEGIN/COMMIT, temp tables) behaves like it would in a terminal client.
type Manager struct {
	connectMu sync.Mutex // serializes Connect/Ensure

	mu       sync.Mutex
	pools    map[string]*pool    // by connection id
	sessions map[string]*session // by tab id
}

type pool struct {
	cfg store.Connection
	db  *sql.DB
}

type session struct {
	connID string
	conn   *sql.Conn
	mu     sync.Mutex // held while a query runs

	cmu    sync.Mutex // guards cancel
	cancel context.CancelFunc
}

func (s *session) setCancel(c context.CancelFunc) {
	s.cmu.Lock()
	s.cancel = c
	s.cmu.Unlock()
}

func (s *session) abort() {
	s.cmu.Lock()
	c := s.cancel
	s.cmu.Unlock()
	if c != nil {
		c()
	}
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{pools: map[string]*pool{}, sessions: map[string]*session{}}
}

// Test opens c, pings it and closes it again.
func Test(ctx context.Context, c store.Connection) (string, error) {
	db, err := Open(c)
	if err != nil {
		return "", err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return "", err
	}
	return serverVersion(ctx, db, c.Driver), nil
}

func serverVersion(ctx context.Context, db *sql.DB, driver string) string {
	var q string
	switch driver {
	case Postgres:
		q = "SHOW server_version"
	case MySQL:
		q = "SELECT VERSION()"
	case SQLite:
		q = "SELECT sqlite_version()"
	}
	var v string
	if err := db.QueryRowContext(ctx, q).Scan(&v); err != nil {
		return ""
	}
	return v
}

// Ensure opens a pool for c unless one is already open.
func (m *Manager) Ensure(ctx context.Context, c store.Connection) error {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()
	if m.IsConnected(c.ID) {
		return nil
	}
	return m.connect(ctx, c)
}

// Connect opens a pool for c, replacing any existing one with the same id.
func (m *Manager) Connect(ctx context.Context, c store.Connection) error {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()
	return m.connect(ctx, c)
}

func (m *Manager) connect(ctx context.Context, c store.Connection) error {
	db, err := Open(c)
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		db.Close()
		return err
	}
	m.Disconnect(c.ID)
	m.mu.Lock()
	m.pools[c.ID] = &pool{cfg: c, db: db}
	m.mu.Unlock()
	return nil
}

// Disconnect closes the pool for id and every session using it.
func (m *Manager) Disconnect(id string) {
	m.mu.Lock()
	p := m.pools[id]
	delete(m.pools, id)
	var closing []*session
	for tab, s := range m.sessions {
		if s.connID == id {
			closing = append(closing, s)
			delete(m.sessions, tab)
		}
	}
	m.mu.Unlock()
	for _, s := range closing {
		s.close()
	}
	if p != nil {
		p.db.Close()
	}
}

// Connected reports which connection ids have an open pool.
func (m *Manager) Connected() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.pools))
	for id := range m.pools {
		ids = append(ids, id)
	}
	return ids
}

// IsConnected reports whether id has an open pool.
func (m *Manager) IsConnected(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.pools[id]
	return ok
}

// DB returns the pool and driver name for connection id.
func (m *Manager) DB(id string) (*sql.DB, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok {
		return nil, "", ErrNotConnected
	}
	return p.db, p.cfg.Driver, nil
}

// Close shuts every pool down.
func (m *Manager) Close() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.pools))
	for id := range m.pools {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Disconnect(id)
	}
}

// CloseSession releases the dedicated connection of tabID, if any.
func (m *Manager) CloseSession(tabID string) {
	m.mu.Lock()
	s := m.sessions[tabID]
	delete(m.sessions, tabID)
	m.mu.Unlock()
	if s != nil {
		s.close()
	}
}

func (s *session) close() {
	s.abort()
	// Wait for a running query to notice the cancellation before closing.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.Close()
}

// getSession returns the session for tabID bound to connID, creating it (and
// dropping a session bound to a different connection) as needed.
func (m *Manager) getSession(ctx context.Context, tabID, connID string) (*session, error) {
	m.mu.Lock()
	s := m.sessions[tabID]
	if s != nil && s.connID == connID {
		m.mu.Unlock()
		return s, nil
	}
	delete(m.sessions, tabID)
	p, ok := m.pools[connID]
	m.mu.Unlock()
	if s != nil {
		s.close()
	}
	if !ok {
		return nil, ErrNotConnected
	}
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	s = &session{connID: connID, conn: conn}
	m.mu.Lock()
	existing := m.sessions[tabID]
	if existing != nil && existing.connID == connID {
		// Lost a race with another caller for the same connection; use theirs.
		m.mu.Unlock()
		conn.Close()
		return existing, nil
	}
	m.sessions[tabID] = s
	m.mu.Unlock()
	if existing != nil {
		existing.close() // bound to a different connection
	}
	return s, nil
}

// Run executes statements in order on the session of tabID. Execution stops
// at the first failing statement unless continueOnError is set. The returned
// slice has one Result per executed statement.
func (m *Manager) Run(ctx context.Context, tabID, connID string, statements []string, maxRows int, continueOnError bool) ([]Result, error) {
	s, err := m.getSession(ctx, tabID, connID)
	if err != nil {
		return nil, err
	}
	if !s.mu.TryLock() {
		return nil, ErrBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	s.setCancel(cancel)
	defer func() {
		cancel()
		s.setCancel(nil)
		s.mu.Unlock()
	}()

	m.mu.Lock()
	driver := ""
	if p := m.pools[connID]; p != nil {
		driver = p.cfg.Driver
	}
	m.mu.Unlock()

	results := make([]Result, 0, len(statements))
	for _, stmt := range statements {
		r := Execute(ctx, s.conn, stmt, maxRows)
		if r.HasResultSet && r.Error == "" && ctx.Err() == nil {
			r.Editable, r.ReadOnly = EditInfo(ctx, s.conn, driver, stmt, r.Columns)
		}
		results = append(results, r)
		if ctx.Err() != nil {
			results[len(results)-1].Error = "Query cancelled"
			break
		}
		if r.Error != "" && !continueOnError {
			break
		}
	}
	if ctx.Err() != nil {
		// A cancelled query can leave the connection unusable; start fresh next time.
		m.mu.Lock()
		if m.sessions[tabID] == s {
			delete(m.sessions, tabID)
		}
		m.mu.Unlock()
		s.conn.Close()
	}
	return results, nil
}

// Cancel aborts the query running in tabID, if any.
func (m *Manager) Cancel(tabID string) error {
	m.mu.Lock()
	s := m.sessions[tabID]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no session for tab")
	}
	s.abort()
	return nil
}
