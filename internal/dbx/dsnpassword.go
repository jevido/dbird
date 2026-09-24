package dbx

import (
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// SplitPassword removes a password embedded in a connection URL or DSN, so it
// can be kept in the password store instead of the settings file. It returns
// dsn unchanged and "" when there is no password or dsn can't be parsed.
func SplitPassword(driver, dsn string) (clean, password string) {
	switch driver {
	case Postgres:
		if strings.Contains(dsn, "://") {
			u, err := url.Parse(dsn)
			if err != nil || u.User == nil {
				return dsn, ""
			}
			pw, ok := u.User.Password()
			if !ok || pw == "" {
				return dsn, ""
			}
			u.User = url.User(u.User.Username())
			return u.String(), pw
		}
		pairs, ok := parseKeywordDSN(dsn)
		if !ok {
			return dsn, ""
		}
		kept := pairs[:0]
		for _, p := range pairs {
			if p[0] == "password" {
				password = p[1]
			} else {
				kept = append(kept, p)
			}
		}
		if password == "" {
			return dsn, ""
		}
		return formatKeywordDSN(kept), password
	case MySQL:
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil || cfg.Passwd == "" {
			return dsn, ""
		}
		password, cfg.Passwd = cfg.Passwd, ""
		return cfg.FormatDSN(), password
	}
	return dsn, ""
}

// withPassword puts password into a connection URL or DSN.
func withPassword(driver, dsn, password string) string {
	switch driver {
	case Postgres:
		if strings.Contains(dsn, "://") {
			u, err := url.Parse(dsn)
			if err != nil {
				return dsn
			}
			name := ""
			if u.User != nil {
				name = u.User.Username()
			}
			u.User = url.UserPassword(name, password)
			return u.String()
		}
		// In keyword/value DSNs a later keyword overrides an earlier one.
		return strings.TrimSpace(dsn) + " password=" + quoteKeywordValue(password)
	case MySQL:
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return dsn
		}
		cfg.Passwd = password
		return cfg.FormatDSN()
	}
	return dsn
}

// parseKeywordDSN splits a libpq keyword/value string such as
// "host=db user=me password='it”s'" into key/value pairs.
func parseKeywordDSN(s string) ([][2]string, bool) {
	var out [][2]string
	i := 0
	skip := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
	}
	for {
		skip()
		if i >= len(s) {
			return out, true
		}
		start := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' {
			i++
		}
		key := s[start:i]
		skip()
		if key == "" || i >= len(s) || s[i] != '=' {
			return nil, false
		}
		i++
		skip()
		var val strings.Builder
		if i < len(s) && s[i] == '\'' {
			i++
			for {
				if i >= len(s) {
					return nil, false
				}
				c := s[i]
				if c == '\\' && i+1 < len(s) {
					val.WriteByte(s[i+1])
					i += 2
					continue
				}
				i++
				if c == '\'' {
					break
				}
				val.WriteByte(c)
			}
		} else {
			for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				val.WriteByte(s[i])
				i++
			}
		}
		out = append(out, [2]string{key, val.String()})
	}
}

func formatKeywordDSN(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p[0] + "=" + quoteKeywordValue(p[1])
	}
	return strings.Join(parts, " ")
}

func quoteKeywordValue(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\r'\\") {
		return v
	}
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}
