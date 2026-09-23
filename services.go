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

// List returns all saved connections.
func (s *ConnectionService) List() []store.Connection {
	return s.store.Connections()
}

// Save creates or updates a connection. An open pool for it is closed so the
// new settings take effect on the next connect.
func (s *ConnectionService) Save(c store.Connection) (store.Connection, error) {
	c.Name = strings.TrimSpace(c.Name)
	if err := validate(c); err != nil {
		return store.Connection{}, err
	}
	if c.ID != "" {
		if old, ok := s.store.Connection(c.ID); ok && old != c {
			s.dbm.Disconnect(c.ID)
		}
	}
	return s.store.SaveConnection(c)
}

// Delete removes a saved connection and closes it if open.
func (s *ConnectionService) Delete(id string) error {
	s.dbm.Disconnect(id)
	return s.store.DeleteConnection(id)
}

// Test tries to connect with c and returns the server version.
func (s *ConnectionService) Test(ctx context.Context, c store.Connection) (string, error) {
	if err := validate(c); err != nil {
		return "", err
	}
	return dbx.Test(ctx, c)
}

// Connect opens the saved connection id.
func (s *ConnectionService) Connect(ctx context.Context, id string) error {
	c, ok := s.store.Connection(id)
	if !ok {
		return errors.New("unknown connection")
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
