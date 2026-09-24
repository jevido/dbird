// Package dbeaver reads saved connections from a DBeaver workspace so they can
// be imported into dbird. It only reads DBeaver's files, never writes them.
package dbeaver

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"dbird/internal/dbx"
	"dbird/internal/store"
)

// credentialsKey is the AES key DBeaver uses for credentials-config.json. It
// is the same in every DBeaver install, so this is obfuscation, not secrecy.
const credentialsKey = "babb4a9f774ab853c96c2d653dfe544a"

// Candidate is one DBeaver connection, converted as far as dbird supports it.
type Candidate struct {
	// Key identifies the connection within the scan (project/id).
	Key     string `json:"key"`
	Project string `json:"project"`
	Folder  string `json:"folder"`
	// Connection is the dbird connection to create. Its Name includes the
	// DBeaver folder, e.g. "Prod / billing".
	Connection store.Connection `json:"connection"`
	// Problem is set when the connection can't be imported at all.
	Problem string `json:"problem"`
	// Warnings describe settings that are dropped on import.
	Warnings []string `json:"warnings"`
	// Existing names the saved dbird connection that already points at the
	// same database, if any. Scan leaves it empty.
	Existing string `json:"existing"`
}

// DefaultWorkspaces returns the DBeaver workspace directories that exist on
// this machine, most likely first.
func DefaultWorkspaces() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		dirs = []string{filepath.Join(home, "Library", "DBeaverData", "workspace6")}
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			dirs = []string{filepath.Join(appData, "DBeaverData", "workspace6")}
		}
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		dirs = []string{
			filepath.Join(data, "DBeaverData", "workspace6"),
			filepath.Join(home, ".var", "app", "io.dbeaver.DBeaverCommunity", "data", "DBeaverData", "workspace6"),
			filepath.Join(home, "snap", "dbeaver-ce", "current", ".local", "share", "DBeaverData", "workspace6"),
		}
	}
	var out []string
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// Scan reads every project under dir. dir may be a workspace (workspace6), a
// project directory, or a project's .dbeaver directory.
func Scan(dir string) ([]Candidate, error) {
	projects, err := projectDirs(dir)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("no DBeaver connections found in %s", dir)
	}
	var out []Candidate
	for _, p := range projects {
		cands, err := readProject(p.dir, p.name)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", p.name, err)
		}
		out = append(out, cands...)
	}
	return out, nil
}

type project struct{ name, dir string }

// projectDirs finds the .dbeaver directories holding data-sources files.
func projectDirs(dir string) ([]project, error) {
	has := func(d string) bool {
		m, _ := filepath.Glob(filepath.Join(d, "data-sources*.json"))
		return len(m) > 0
	}
	if has(dir) {
		return []project{{filepath.Base(filepath.Dir(dir)), dir}}, nil
	}
	if d := filepath.Join(dir, ".dbeaver"); has(d) {
		return []project{{filepath.Base(dir), d}}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []project
	for _, e := range entries {
		if d := filepath.Join(dir, e.Name(), ".dbeaver"); e.IsDir() && has(d) {
			out = append(out, project{e.Name(), d})
		}
	}
	return out, nil
}

type dataSources struct {
	Connections map[string]dsConnection `json:"connections"`
}

type dsConnection struct {
	Provider      string `json:"provider"`
	Driver        string `json:"driver"`
	Name          string `json:"name"`
	Folder        string `json:"folder"`
	SavePassword  bool   `json:"save-password"`
	Configuration struct {
		Host              string               `json:"host"`
		Port              json.RawMessage      `json:"port"`
		Database          string               `json:"database"`
		URL               string               `json:"url"`
		ConfigurationType string               `json:"configurationType"`
		Type              string               `json:"type"`
		User              string               `json:"user"`
		Password          string               `json:"password"`
		Handlers          map[string]dsHandler `json:"handlers"`
	} `json:"configuration"`
}

type dsHandler struct {
	Enabled    bool              `json:"enabled"`
	Properties map[string]string `json:"properties"`
}

// credentials maps connection id -> section ("#connection", ...) -> field.
type credentials map[string]map[string]map[string]string

func readProject(dir, name string) ([]Candidate, error) {
	files, err := filepath.Glob(filepath.Join(dir, "data-sources*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	creds, credErr := readCredentials(filepath.Join(dir, "credentials-config.json"))

	var out []Candidate
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var ds dataSources
		if err := json.Unmarshal(b, &ds); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(f), err)
		}
		ids := make([]string, 0, len(ds.Connections))
		for id := range ds.Connections {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			c := convert(ds.Connections[id], creds[id]["#connection"])
			c.Key = name + "/" + id
			c.Project = name
			if credErr != nil && ds.Connections[id].SavePassword {
				c.Warnings = append(c.Warnings, "saved password could not be read: "+credErr.Error())
			}
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Connection.Name) < strings.ToLower(out[j].Connection.Name)
	})
	return out, nil
}

// readCredentials decrypts credentials-config.json. A missing file is not an
// error: DBeaver doesn't write it when no passwords are saved.
func readCredentials(path string) (credentials, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentials{}, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := decrypt(b)
	if err != nil {
		return nil, err
	}
	var c credentials
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return c, nil
}

// decrypt reverses DBeaver's AES-128-CBC encryption: a 16-byte IV followed by
// the PKCS#7-padded ciphertext.
func decrypt(b []byte) ([]byte, error) {
	key, _ := hex.DecodeString(credentialsKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(b) < 2*aes.BlockSize || len(b)%aes.BlockSize != 0 {
		return nil, errors.New("credentials file has an unexpected size")
	}
	iv, data := b[:aes.BlockSize], append([]byte(nil), b[aes.BlockSize:]...)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(data, data)
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize {
		return nil, errors.New("credentials file could not be decrypted")
	}
	return data[:len(data)-pad], nil
}

// driverFor maps a DBeaver provider/driver to a dbird driver, or "".
func driverFor(provider, driver string) string {
	p, d := strings.ToLower(provider), strings.ToLower(driver)
	switch {
	case p == "postgresql":
		return dbx.Postgres
	case p == "mysql":
		return dbx.MySQL
	case p == "sqlite", strings.Contains(d, "sqlite"):
		return dbx.SQLite
	}
	return ""
}

// typeColors maps DBeaver's built-in connection types to dbird color tags.
var typeColors = map[string]string{
	"prod": "#f44336",
	"test": "#ff9800",
}

func convert(dc dsConnection, cred map[string]string) Candidate {
	cfg := dc.Configuration
	c := store.Connection{
		Name:     dc.Name,
		Host:     cfg.Host,
		Port:     parsePort(cfg.Port),
		Database: cfg.Database,
		User:     cfg.User,
		Password: cfg.Password,
		Color:    typeColors[cfg.Type],
	}
	if cred["user"] != "" {
		c.User = cred["user"]
	}
	if cred["password"] != "" {
		c.Password = cred["password"]
	}
	cand := Candidate{Folder: dc.Folder}
	if dc.Folder != "" {
		c.Name = strings.ReplaceAll(dc.Folder, "/", " / ") + " / " + dc.Name
	}

	c.Driver = driverFor(dc.Provider, dc.Driver)
	if c.Driver == "" {
		cand.Connection = c
		cand.Problem = fmt.Sprintf("%s isn't supported by dbird", driverLabel(dc))
		return cand
	}

	// URL-only connections, and SQLite, keep their details in the JDBC URL.
	if cfg.ConfigurationType == "URL" || (c.Driver != dbx.SQLite && c.Host == "") || (c.Driver == dbx.SQLite && c.Database == "") {
		if err := fromJDBC(&c, cfg.URL); err != nil {
			cand.Connection = c
			cand.Problem = err.Error()
			return cand
		}
	}

	for name, h := range cfg.Handlers {
		if !h.Enabled {
			continue
		}
		switch {
		case strings.Contains(name, "ssh"):
			cand.Warnings = append(cand.Warnings, "uses an SSH tunnel, which dbird doesn't support")
		case strings.Contains(name, "proxy"):
			cand.Warnings = append(cand.Warnings, "uses a proxy, which dbird doesn't support")
		case strings.Contains(name, "ssl"):
			if c.Driver == dbx.Postgres {
				c.SSLMode = sslMode(h.Properties)
			} else {
				cand.Warnings = append(cand.Warnings, "SSL settings aren't imported")
			}
		}
	}
	if c.Driver == dbx.Postgres && c.SSLMode == "" {
		c.SSLMode = "prefer"
	}
	if dc.SavePassword && c.Password == "" && c.Driver != dbx.SQLite {
		cand.Warnings = append(cand.Warnings, "no saved password found")
	}
	sort.Strings(cand.Warnings)
	cand.Connection = c
	return cand
}

func driverLabel(dc dsConnection) string {
	if dc.Driver != "" {
		return dc.Driver
	}
	return dc.Provider
}

func parsePort(raw json.RawMessage) int {
	s := strings.Trim(string(raw), `" `)
	n, _ := strconv.Atoi(s)
	return n
}

func sslMode(props map[string]string) string {
	for _, k := range []string{"sslMode", "sslmode", "ssl.mode"} {
		if v := props[k]; v != "" {
			return v
		}
	}
	return "require"
}

// fromJDBC fills host, port and database from a JDBC URL such as
// jdbc:postgresql://host:5432/db or jdbc:sqlite:/path/to/file.db.
func fromJDBC(c *store.Connection, jdbc string) error {
	raw, ok := strings.CutPrefix(strings.TrimSpace(jdbc), "jdbc:")
	if !ok {
		return fmt.Errorf("couldn't read the connection URL %q", jdbc)
	}
	if c.Driver == dbx.SQLite {
		path := strings.TrimPrefix(raw, "sqlite:")
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		if path == "" {
			return errors.New("SQLite connection has no file path")
		}
		c.Database = path
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("couldn't read the connection URL %q", jdbc)
	}
	// Some JDBC URLs list several hosts; dbird connects to the first.
	host := strings.Split(u.Host, ",")[0]
	if h, p, ok := strings.Cut(host, ":"); ok && !strings.Contains(p, ":") {
		c.Host = h
		c.Port, _ = strconv.Atoi(p)
	} else {
		c.Host = host
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		c.Database = db
	}
	if c.User == "" {
		c.User = u.Query().Get("user")
	}
	if c.Password == "" {
		c.Password = u.Query().Get("password")
	}
	if c.Driver == dbx.Postgres {
		if m := u.Query().Get("sslmode"); m != "" {
			c.SSLMode = m
		}
	}
	return nil
}
