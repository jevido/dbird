package dbeaver

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// encrypt mirrors how DBeaver writes credentials-config.json.
func encrypt(t *testing.T, plain []byte) []byte {
	t.Helper()
	key, _ := hex.DecodeString(credentialsKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	data := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := []byte("0123456789abcdef")
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(data, data)
	return append(iv, data...)
}

const dataSourcesJSON = `{
  "folders": {"Prod": {}, "Prod/EU": {"parent": "Prod"}},
  "connections": {
    "postgres-jdbc-1": {
      "provider": "postgresql", "driver": "postgres-jdbc", "name": "billing",
      "folder": "Prod/EU", "save-password": true,
      "configuration": {
        "host": "db.example.com", "port": "5433", "database": "billing",
        "url": "jdbc:postgresql://db.example.com:5433/billing",
        "configurationType": "MANUAL", "type": "prod",
        "handlers": {
          "postgre_ssl": {"enabled": true, "properties": {"sslMode": "verify-full"}},
          "ssh_tunnel": {"enabled": true}
        }
      }
    },
    "mysql8-1": {
      "provider": "mysql", "driver": "mysql8", "name": "shop", "save-password": true,
      "configuration": {
        "url": "jdbc:mysql://shop.local:3307/shop?useSSL=false",
        "configurationType": "URL", "type": "dev"
      }
    },
    "sqlite-1": {
      "provider": "sqlite", "driver": "sqlite_jdbc", "name": "local",
      "configuration": {"url": "jdbc:sqlite:/home/me/app.db", "configurationType": "URL"}
    },
    "oracle-1": {
      "provider": "oracle", "driver": "oracle_thin", "name": "legacy",
      "configuration": {"host": "ora", "port": "1521"}
    }
  }
}`

const credentialsJSON = `{
  "postgres-jdbc-1": {"#connection": {"user": "billing_ro", "password": "s3cret"}},
  "mysql8-1": {"#connection": {"user": "root", "password": "hunter2"}}
}`

func writeWorkspace(t *testing.T, creds []byte) string {
	t.Helper()
	ws := t.TempDir()
	dir := filepath.Join(ws, "General", ".dbeaver")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data-sources.json"), []byte(dataSourcesJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if creds != nil {
		if err := os.WriteFile(filepath.Join(dir, "credentials-config.json"), creds, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func byName(t *testing.T, cands []Candidate, name string) Candidate {
	t.Helper()
	for _, c := range cands {
		if c.Connection.Name == name {
			return c
		}
	}
	t.Fatalf("no candidate named %q", name)
	return Candidate{}
}

func TestScan(t *testing.T) {
	ws := writeWorkspace(t, encrypt(t, []byte(credentialsJSON)))
	cands, err := Scan(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 4 {
		t.Fatalf("got %d candidates, want 4", len(cands))
	}

	pg := byName(t, cands, "Prod / EU / billing")
	c := pg.Connection
	if c.Driver != "postgres" || c.Host != "db.example.com" || c.Port != 5433 || c.Database != "billing" ||
		c.User != "billing_ro" || c.Password != "s3cret" || c.SSLMode != "verify-full" || c.Color != "#f44336" {
		t.Errorf("postgres: %+v", c)
	}
	if pg.Problem != "" || len(pg.Warnings) != 1 || !strings.Contains(pg.Warnings[0], "SSH") {
		t.Errorf("postgres problem %q warnings %v", pg.Problem, pg.Warnings)
	}
	if pg.Key != "General/postgres-jdbc-1" || pg.Project != "General" || pg.Folder != "Prod/EU" {
		t.Errorf("postgres key %q project %q folder %q", pg.Key, pg.Project, pg.Folder)
	}

	my := byName(t, cands, "shop").Connection
	if my.Driver != "mysql" || my.Host != "shop.local" || my.Port != 3307 || my.Database != "shop" ||
		my.User != "root" || my.Password != "hunter2" || my.Color != "" {
		t.Errorf("mysql: %+v", my)
	}

	sl := byName(t, cands, "local")
	if sl.Connection.Driver != "sqlite" || sl.Connection.Database != "/home/me/app.db" || sl.Problem != "" {
		t.Errorf("sqlite: %+v", sl)
	}

	if ora := byName(t, cands, "legacy"); !strings.Contains(ora.Problem, "oracle_thin") {
		t.Errorf("oracle problem = %q", ora.Problem)
	}
}

func TestScanAcceptsProjectAndDotDir(t *testing.T) {
	ws := writeWorkspace(t, nil)
	for _, dir := range []string{filepath.Join(ws, "General"), filepath.Join(ws, "General", ".dbeaver")} {
		cands, err := Scan(dir)
		if err != nil || len(cands) != 4 || cands[0].Project != "General" {
			t.Errorf("Scan(%s) = %d candidates, err %v", dir, len(cands), err)
		}
	}
}

func TestScanWithoutCredentials(t *testing.T) {
	cands, err := Scan(writeWorkspace(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	my := byName(t, cands, "shop")
	if my.Connection.Password != "" || len(my.Warnings) != 1 || my.Warnings[0] != "no saved password found" {
		t.Errorf("mysql without credentials: %+v", my)
	}
}

func TestScanCorruptCredentials(t *testing.T) {
	cands, err := Scan(writeWorkspace(t, []byte("not encrypted at all")))
	if err != nil {
		t.Fatal(err)
	}
	my := byName(t, cands, "shop")
	if !strings.Contains(strings.Join(my.Warnings, "|"), "could not be read") {
		t.Errorf("warnings = %v", my.Warnings)
	}
}

func TestScanEmptyDir(t *testing.T) {
	if _, err := Scan(t.TempDir()); err == nil {
		t.Error("expected an error for a directory without DBeaver projects")
	}
}
