package changelog

import "testing"

func TestBetween(t *testing.T) {
	entries := []Entry{{Version: "0.3.0"}, {Version: "0.2.1"}, {Version: "0.2.0"}, {Version: "0.2.0-beta.1"}, {Version: "0.1.0"}}
	got := Between(entries, "0.1.0", "0.2.1")
	if len(got) != 3 || got[0].Version != "0.2.1" || got[2].Version != "0.2.0-beta.1" {
		t.Fatalf("got %+v", got)
	}
	if len(Between(entries, "0.3.0", "0.3.0")) != 0 {
		t.Fatal("nothing new expected")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0}, {"v0.1.1", "0.1.0", 1}, {"0.10.0", "0.9.9", 1},
		{"1.0.0-beta.1", "1.0.0", -1}, {"1.0.0", "1.0.0-rc.1", 1}, {"", "0.0.1", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestEmbeddedParses(t *testing.T) {
	if _, err := Parse(data); err != nil {
		t.Fatal(err)
	}
}
