package main

import (
	"errors"
	"log"
	"sync"

	"dbird/internal/dbx"
	"dbird/internal/secret"
	"dbird/internal/store"
)

// errPasswordRequired is returned when connecting needs a password dbird
// doesn't have. The frontend recognizes the message and asks for it.
var errPasswordRequired = errors.New("password required")

// passwords keeps connection passwords out of the settings file: in the OS
// password store, or, without one, only in memory for this session.
type passwords struct {
	keeper secret.Keeper
	mu     sync.Mutex
	// session holds passwords typed this session, and ones read from the
	// password store, so it isn't asked again on every connect.
	session map[string]string
	// plaintextLeft counts connections whose password is still in the
	// settings file because the password store wasn't available on startup.
	plaintextLeft int
}

func newPasswords(k secret.Keeper) *passwords {
	return &passwords{keeper: k, session: map[string]string{}}
}

func (p *passwords) remember(id, pw string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.session[id] = pw
}

func (p *passwords) forget(id string) {
	p.mu.Lock()
	delete(p.session, id)
	p.mu.Unlock()
	if err := p.keeper.Delete(id); err != nil && !errors.Is(err, secret.ErrNotFound) {
		log.Printf("delete password of %s: %v", id, err)
	}
}

// lookup returns the password of c: typed this session, or from the password
// store. ok is false when c has no password to find.
func (p *passwords) lookup(c store.Connection) (pw string, ok bool) {
	p.mu.Lock()
	pw, ok = p.session[c.ID]
	p.mu.Unlock()
	if ok {
		return pw, true
	}
	if c.Password != "" { // not moved out of the settings file yet
		return c.Password, true
	}
	if c.HasPassword {
		if pw, err := p.keeper.Get(c.ID); err == nil {
			p.remember(c.ID, pw)
			return pw, true
		}
	}
	return "", false
}

// resolve fills in c's password for connecting, or returns
// errPasswordRequired when it has to be asked for.
func (p *passwords) resolve(c store.Connection) (store.Connection, error) {
	if !c.HasPassword && !c.AskPassword && c.Password == "" {
		return c, nil
	}
	pw, ok := p.lookup(c)
	if !ok {
		return c, errPasswordRequired
	}
	c.Password = pw
	return c, nil
}

// store saves pw for connection c, setting HasPassword or, without a password
// store, AskPassword (and keeping it in memory for this session).
func (p *passwords) store(c *store.Connection, pw string) {
	p.remember(c.ID, pw)
	err := p.keeper.Set(c.ID, pw)
	c.HasPassword = err == nil
	c.AskPassword = err != nil
	if err != nil {
		log.Printf("save password of %s: %v", c.ID, err)
	}
}

// clean returns c as it may be shown to the frontend: without its password,
// also when one is still embedded in its URL.
func clean(c store.Connection) store.Connection {
	c.Password = ""
	if c.URL != "" {
		c.URL, _ = dbx.SplitPassword(c.Driver, c.URL)
	}
	return c
}

// legacyPassword returns a password older versions saved in the settings
// file, directly or inside the URL.
func legacyPassword(c store.Connection) string {
	if c.Password != "" {
		return c.Password
	}
	if c.URL != "" {
		_, pw := dbx.SplitPassword(c.Driver, c.URL)
		return pw
	}
	return ""
}

// migratePasswords moves passwords that older versions saved in the settings
// file into the password store. Without a password store they stay where they
// are, so a password store that isn't running yet doesn't cost anyone their
// passwords; the next start tries again.
func (s *ConnectionService) migratePasswords() {
	left := 0
	for _, c := range s.store.Connections() {
		pw := legacyPassword(c)
		if pw == "" {
			continue
		}
		if err := s.pw.keeper.Set(c.ID, pw); err != nil {
			log.Printf("move password of %s to the password store: %v", c.Name, err)
			left++
			continue
		}
		c = clean(c)
		c.HasPassword, c.AskPassword = true, false
		if _, err := s.store.SaveConnection(c); err != nil {
			log.Printf("save %s: %v", c.Name, err)
		}
	}
	s.pw.mu.Lock()
	s.pw.plaintextLeft = left
	s.pw.mu.Unlock()
}

// PasswordStoreInfo describes where connection passwords are kept.
type PasswordStoreInfo struct {
	// Available is false when there is no usable password store; passwords
	// are then asked for on connect.
	Available bool `json:"available"`
	// Name is how the password store is called on this OS.
	Name string `json:"name"`
	// PlaintextLeft counts connections whose password older versions saved
	// in the settings file and that couldn't be moved to the password store.
	PlaintextLeft int `json:"plaintextLeft"`
}

// PasswordStore reports whether the OS password store can be used.
func (s *ConnectionService) PasswordStore() PasswordStoreInfo {
	s.pw.mu.Lock()
	left := s.pw.plaintextLeft
	s.pw.mu.Unlock()
	return PasswordStoreInfo{Available: secret.Available(s.pw.keeper), Name: secret.StoreName(), PlaintextLeft: left}
}

// ProvidePassword sets the password of connection id after it was asked for
// on connect. With remember it is saved in the password store; otherwise, or
// without a password store, it is kept until dbird quits.
func (s *ConnectionService) ProvidePassword(id, password string, remember bool) error {
	c, ok := s.store.Connection(id)
	if !ok {
		return errors.New("unknown connection")
	}
	s.pw.remember(id, password)
	if !remember {
		return nil
	}
	s.pw.store(&c, password)
	_, err := s.store.SaveConnection(clean(c))
	return err
}

// ForgetSessionPassword drops a password given with ProvidePassword that
// turned out to be wrong, so the next connect asks again.
func (s *ConnectionService) ForgetSessionPassword(id string) {
	s.pw.mu.Lock()
	delete(s.pw.session, id)
	s.pw.mu.Unlock()
}
