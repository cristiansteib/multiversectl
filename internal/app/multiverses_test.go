package app

import "testing"

func TestSelect(t *testing.T) {
	c := Config{Multiverses: map[string]MultiverseEntry{"a": {Root: "/w/a"}, "b": {Root: "/w/b"}}}
	if e, _, _ := c.Select("b", "/elsewhere"); e.Root != "/w/b" {
		t.Fatalf("explicit name ignored: %+v", e)
	}
	if e, ok, err := c.Select("", "/w/a/api/src"); err != nil || !ok || e.Root != "/w/a" {
		t.Fatalf("the folder you are in should pick its multiverse: %+v, %v, %v", e, ok, err)
	}
	if _, _, err := c.Select("zzz", "/w/a"); err == nil {
		t.Fatal("an unknown multiverse must fail, not fall back to the folder")
	}
	if _, _, err := c.Select("", "/elsewhere"); err == nil {
		t.Fatal("several multiverses and a folder in none of them must say which to name")
	}
	if _, ok, err := (Config{}).Select("", "/anywhere"); ok || err != nil {
		t.Fatalf("empty config: ok=%v err=%v", ok, err)
	}
}

func TestSelectTakesTheOnlyOne(t *testing.T) {
	c := Config{Multiverses: map[string]MultiverseEntry{"a": {Root: "/w/a"}}}
	if e, ok, err := c.Select("", "/elsewhere"); err != nil || !ok || e.Root != "/w/a" {
		t.Fatalf("the only multiverse is the one: %+v, %v, %v", e, ok, err)
	}
}

func TestSelectPrefersTheDeeperRoot(t *testing.T) {
	c := Config{Multiverses: map[string]MultiverseEntry{"outer": {Root: "/w"}, "inner": {Root: "/w/inner"}}}
	if e, _, _ := c.Select("", "/w/inner/api"); e.Root != "/w/inner" {
		t.Fatalf("got %+v", e)
	}
}
