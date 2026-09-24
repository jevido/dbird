package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dbird/internal/dbx"
	"dbird/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ConnectionService manages saved connections, open pools and schema metadata.
type ConnectionService struct {
	store *store.Store
	dbm   *dbx.Manager
	pw    *passwords
}

func validate(c store.Connection) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("name is required")
	}
	switch c.Driver {
	case dbx.Postgres, dbx.MySQL, dbx.SQLite:
	default:
		return fmt.Errorf("unsupported driver %q", c.Driver)
	}
	switch c.Completion {
	case dbx.CompletionAuto, dbx.CompletionPreload, dbx.CompletionLookup, dbx.CompletionOff:
	default:
		return fmt.Errorf("unknown autocomplete mode %q", c.Completion)
	}
	if c.Port < 0 || c.Port > 65535 {
		return errors.New("port must be between 0 and 65535")
	}
	return nil
}

// ServiceShutdown closes every open connection when the app quits.
func (s *ConnectionService) ServiceShutdown() error {
	s.dbm.Close()
	return nil
}

// List returns all saved connections, without passwords.
func (s *ConnectionService) List() []store.Connection {
	conns := s.store.Connections()
	for i := range conns {
		conns[i] = clean(conns[i])
	}
	return conns
}

// Save creates or updates a connection. Its password goes to the OS password
// store (see passwords). An open pool for it is closed so the new settings
// take effect on the next connect.
func (s *ConnectionService) Save(c store.Connection) (store.Connection, error) {
	c.Name = strings.TrimSpace(c.Name)
	if err := validate(c); err != nil {
		return store.Connection{}, err
	}
	return s.save(c)
}

// save stores a validated connection. A password is taken from c.Password or
// from c.URL; an empty one keeps the saved password unless c.ClearPassword is
// set, and a duplicate (c.CopyFrom) gets the password of its original.
func (s *ConnectionService) save(c store.Connection) (store.Connection, error) {
	var old store.Connection
	existed := false
	if c.ID != "" {
		old, existed = s.store.Connection(c.ID)
	} else {
		c.ID = store.NewID()
	}
	pw, clear, copyFrom := c.Password, c.ClearPassword, c.CopyFrom
	c.Password, c.ClearPassword, c.CopyFrom = "", false, ""
	if c.URL != "" {
		var inURL string
		c.URL, inURL = dbx.SplitPassword(c.Driver, c.URL)
		if pw == "" {
			pw = inURL
		}
	}

	hadPassword := existed && (old.HasPassword || old.AskPassword || legacyPassword(old) != "")
	switch {
	case clear || c.Driver == dbx.SQLite:
		pw = ""
		if hadPassword {
			s.pw.forget(c.ID)
		}
		c.HasPassword, c.AskPassword = false, false
	case pw != "":
	case copyFrom != "":
		if src, ok := s.store.Connection(copyFrom); ok {
			pw, _ = s.pw.lookup(src)
		}
	case existed:
		c.HasPassword, c.AskPassword = old.HasPassword, old.AskPassword
		pw = legacyPassword(old) // still in the settings file: move it now
	}

	changed := false
	if pw != "" {
		if existed {
			cur, ok := s.pw.lookup(old)
			changed = !ok || cur != pw
		}
		s.pw.store(&c, pw)
	}
	if existed {
		o := clean(old)
		o.HasPassword, o.AskPassword = c.HasPassword, c.AskPassword
		if changed || clear || o != c {
			s.dbm.Disconnect(c.ID)
		}
	}
	return s.store.SaveConnection(c)
}

// Delete removes a saved connection and its password, and closes it if open.
func (s *ConnectionService) Delete(id string) error {
	s.dbm.Disconnect(id)
	if c, ok := s.store.Connection(id); ok && (c.HasPassword || c.AskPassword) {
		s.pw.forget(id)
	}
	return s.store.DeleteConnection(id)
}

// Test tries to connect with c and returns the server version. Without a
// typed password it uses the saved one of c (or of the connection c is a
// duplicate of).
func (s *ConnectionService) Test(ctx context.Context, c store.Connection) (string, error) {
	if err := validate(c); err != nil {
		return "", err
	}
	_, inURL := dbx.SplitPassword(c.Driver, c.URL)
	if c.Password == "" && inURL == "" && !c.ClearPassword {
		src := c.ID
		if src == "" {
			src = c.CopyFrom
		}
		if saved, ok := s.store.Connection(src); ok {
			c.Password, _ = s.pw.lookup(saved)
		}
	}
	return dbx.Test(ctx, c)
}

// Connect opens the saved connection id. It fails with errPasswordRequired
// when the password has to be asked for (see ProvidePassword).
func (s *ConnectionService) Connect(ctx context.Context, id string) error {
	c, ok := s.store.Connection(id)
	if !ok {
		return errors.New("unknown connection")
	}
	c, err := s.pw.resolve(c)
	if err != nil {
		return err
	}
	return s.dbm.Connect(ctx, c)
}

// Disconnect closes the connection id and all tab sessions using it.
func (s *ConnectionService) Disconnect(id string) {
	s.dbm.Disconnect(id)
}

// Connected returns the ids of open connections.
func (s *ConnectionService) Connected() []string {
	return s.dbm.Connected()
}

func (s *ConnectionService) ensure(ctx context.Context, id string) error {
	if s.dbm.IsConnected(id) {
		return nil
	}
	c, ok := s.store.Connection(id)
	if !ok {
		return errors.New("unknown connection")
	}
	c, err := s.pw.resolve(c)
	if err != nil {
		return err
	}
	return s.dbm.Ensure(ctx, c)
}

// Schemas lists schemas (databases for MySQL) of connection id.
func (s *ConnectionService) Schemas(ctx context.Context, id string) ([]string, error) {
	if err := s.ensure(ctx, id); err != nil {
		return nil, err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return nil, err
	}
	return dbx.Schemas(ctx, db, driver)
}

// DefaultSchema returns the schema unqualified names resolve to.
func (s *ConnectionService) DefaultSchema(ctx context.Context, id string) (string, error) {
	if err := s.ensure(ctx, id); err != nil {
		return "", err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return "", err
	}
	return dbx.DefaultSchema(ctx, db, driver)
}

// Tables lists tables and views in schema.
func (s *ConnectionService) Tables(ctx context.Context, id, schema string) ([]dbx.TableInfo, error) {
	if err := s.ensure(ctx, id); err != nil {
		return nil, err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return nil, err
	}
	return dbx.Tables(ctx, db, driver, schema)
}

// TablePage is one page of the sidebar's table list.
type TablePage struct {
	Tables []dbx.TableInfo `json:"tables"`
	// Total is the number of matching tables; only set for the first page.
	Total int `json:"total"`
}

// TablesPage lists up to limit (max 1000) tables in schema whose name
// contains filter, starting after the name after. The first page (after == "")
// also reports the total, so the sidebar can offer to load more.
func (s *ConnectionService) TablesPage(ctx context.Context, id, schema, filter, after string, limit int) (TablePage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if err := s.ensure(ctx, id); err != nil {
		return TablePage{}, err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return TablePage{}, err
	}
	tables, err := dbx.TablesPage(ctx, db, driver, schema, filter, after, limit)
	if err != nil {
		return TablePage{}, err
	}
	page := TablePage{Tables: tables}
	if after == "" {
		page.Total = len(tables)
		if len(tables) == limit {
			if page.Total, err = dbx.TableCountMatching(ctx, db, driver, schema, filter); err != nil {
				return TablePage{}, err
			}
		}
	}
	return page, nil
}

// Columns lists the columns of schema.table.
func (s *ConnectionService) Columns(ctx context.Context, id, schema, table string) ([]dbx.ColumnInfo, error) {
	if err := s.ensure(ctx, id); err != nil {
		return nil, err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return nil, err
	}
	return dbx.Columns(ctx, db, driver, schema, table)
}

// CompletionSetup tells the editor how to autocomplete for a connection.
type CompletionSetup struct {
	// Mode is "preload", "lookup" or "off" (automatic is resolved here).
	Mode   string `json:"mode"`
	Schema string `json:"schema"`
	// Columns holds table -> column names in preload mode.
	Columns    map[string][]string `json:"columns"`
	TableCount int                 `json:"tableCount"`
}

// Completion prepares autocompletion for connection id according to its
// Autocomplete setting. In automatic mode schemas with up to
// dbx.PreloadTableLimit tables are loaded up front; larger ones are looked up
// as the user types.
func (s *ConnectionService) Completion(ctx context.Context, id string) (CompletionSetup, error) {
	c, ok := s.store.Connection(id)
	if !ok {
		return CompletionSetup{}, errors.New("unknown connection")
	}
	if c.Completion == dbx.CompletionOff {
		return CompletionSetup{Mode: dbx.CompletionOff}, nil
	}
	if err := s.ensure(ctx, id); err != nil {
		return CompletionSetup{}, err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return CompletionSetup{}, err
	}
	schema, err := dbx.DefaultSchema(ctx, db, driver)
	if err != nil || schema == "" {
		return CompletionSetup{Mode: dbx.CompletionOff}, err
	}
	out := CompletionSetup{Mode: c.Completion, Schema: schema}
	if out.Mode == dbx.CompletionAuto {
		n, err := dbx.TableCount(ctx, db, driver, schema)
		if err != nil {
			return CompletionSetup{}, err
		}
		out.TableCount = n
		out.Mode = dbx.CompletionLookup
		if n <= dbx.PreloadTableLimit {
			out.Mode = dbx.CompletionPreload
		}
	}
	if out.Mode == dbx.CompletionPreload {
		out.Columns, err = dbx.SchemaColumns(ctx, db, driver, schema)
		if err != nil {
			return CompletionSetup{}, err
		}
	}
	return out, nil
}

// CompleteTables returns up to 100 table names in schema starting with prefix.
func (s *ConnectionService) CompleteTables(ctx context.Context, id, schema, prefix string) ([]string, error) {
	if err := s.ensure(ctx, id); err != nil {
		return nil, err
	}
	db, driver, err := s.dbm.DB(id)
	if err != nil {
		return nil, err
	}
	return dbx.TablesByPrefix(ctx, db, driver, schema, prefix, 100)
}

// TableColumns returns the column names of schema.table, for autocompletion.
func (s *ConnectionService) TableColumns(ctx context.Context, id, schema, table string) ([]string, error) {
	cols, err := s.Columns(ctx, id, schema, table)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names, nil
}

// PickSQLiteFile shows a file dialog and returns the chosen path ("" if cancelled).
func (s *ConnectionService) PickSQLiteFile() (string, error) {
	return application.Get().Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "Choose SQLite database",
		CanChooseFiles:       true,
		CanCreateDirectories: true,
		AllowsOtherFileTypes: true,
		Filters: []application.FileFilter{
			{DisplayName: "SQLite databases", Pattern: "*.db;*.sqlite;*.sqlite3;*.db3"},
			{DisplayName: "All files", Pattern: "*"},
		},
	}).PromptForSingleSelection()
}

// QueryService executes SQL from editor tabs.
type QueryService struct {
	conns *ConnectionService
	dbm   *dbx.Manager
}

// Run executes statements for tabID on connection connID, connecting first if
// needed. At most maxRows rows are returned per result set.
func (s *QueryService) Run(ctx context.Context, tabID, connID string, statements []string, maxRows int, continueOnError bool) ([]dbx.Result, error) {
	if connID == "" {
		return nil, errors.New("select a connection for this tab first")
	}
	if len(statements) == 0 {
		return nil, errors.New("nothing to execute")
	}
	if maxRows <= 0 || maxRows > 100000 {
		maxRows = 1000
	}
	if err := s.conns.ensure(ctx, connID); err != nil {
		return nil, err
	}
	return s.dbm.Run(ctx, tabID, connID, statements, maxRows, continueOnError)
}

// SaveEdits writes cells edited in the result grid back to their table, in
// one transaction on connection connID. It returns the number of rows updated.
func (s *QueryService) SaveEdits(ctx context.Context, connID string, req dbx.EditRequest) (int, error) {
	if len(req.Rows) == 0 {
		return 0, errors.New("nothing to save")
	}
	if err := s.conns.ensure(ctx, connID); err != nil {
		return 0, err
	}
	db, driver, err := s.dbm.DB(connID)
	if err != nil {
		return 0, err
	}
	return dbx.SaveEdits(ctx, db, driver, req)
}

// Cancel aborts the running query of tabID.
func (s *QueryService) Cancel(tabID string) error {
	return s.dbm.Cancel(tabID)
}

// CloseTab releases the dedicated connection held by tabID.
func (s *QueryService) CloseTab(tabID string) {
	s.dbm.CloseSession(tabID)
}

// WorkspaceService persists open tabs between runs.
type WorkspaceService struct {
	store *store.Store
}

// Load returns the saved workspace.
func (s *WorkspaceService) Load() store.Workspace {
	return s.store.Workspace()
}

// Save stores the workspace.
func (s *WorkspaceService) Save(w store.Workspace) error {
	return s.store.SaveWorkspace(w)
}
