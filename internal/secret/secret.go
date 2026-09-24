// Package secret keeps connection passwords in the operating system's password
// store: the Secret Service (GNOME Keyring, KWallet, KeePassXC) on Linux, the
// Keychain on macOS and the Credential Manager on Windows.
package secret

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/zalando/go-keyring"
)

// service is the name entries are filed under in the password store.
const service = "dbird"

var (
	// ErrNotFound means the password store has no entry for the key.
	ErrNotFound = errors.New("no saved password")
	// ErrUnavailable means there is no usable password store: none is
	// installed or running, or unlocking it was cancelled.
	ErrUnavailable = errors.New("no password store available")
)

// Keeper stores one secret per key.
type Keeper interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// System returns a Keeper backed by the operating system's password store.
func System() Keeper { return systemKeeper{} }

type systemKeeper struct{}

func (systemKeeper) Get(key string) (string, error) {
	v, err := keyring.Get(service, key)
	return v, wrap(err)
}

func (systemKeeper) Set(key, value string) error {
	return wrap(keyring.Set(service, key, value))
}

func (systemKeeper) Delete(key string) error {
	return wrap(keyring.Delete(service, key))
}

func wrap(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	default:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}

// Available reports whether k can be used, by looking up a key that doesn't
// exist.
func Available(k Keeper) bool {
	_, err := k.Get("dbird-availability-check")
	return err == nil || errors.Is(err, ErrNotFound)
}

// StoreName is how the password store is called on this OS, for messages.
func StoreName() string {
	switch runtime.GOOS {
	case "darwin":
		return "the macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "your keyring"
	}
}

// Memory is an in-memory Keeper for tests. Unavailable makes every call fail
// with ErrUnavailable.
type Memory struct {
	mu          sync.Mutex
	m           map[string]string
	Unavailable bool
}

func (k *Memory) Get(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Unavailable {
		return "", ErrUnavailable
	}
	v, ok := k.m[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (k *Memory) Set(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Unavailable {
		return ErrUnavailable
	}
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[key] = value
	return nil
}

func (k *Memory) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Unavailable {
		return ErrUnavailable
	}
	if _, ok := k.m[key]; !ok {
		return ErrNotFound
	}
	delete(k.m, key)
	return nil
}
