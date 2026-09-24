package secret

import (
	"errors"
	"os"
	"testing"
)

func TestSystemRoundTrip(t *testing.T) {
	if os.Getenv("DBIRD_KEYRING_TEST") == "" {
		t.Skip("set DBIRD_KEYRING_TEST=1 to use the real password store")
	}
	k := System()
	if !Available(k) {
		t.Fatal("password store not available")
	}
	const key = "dbird-selftest"
	if err := k.Set(key, "value"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get(key); v != "value" || err != nil {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if err := k.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete: %v", err)
	}
}
