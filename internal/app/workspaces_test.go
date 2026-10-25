package app

import "testing"

func TestNormalizeFoldsLegacyConfig(t *testing.T) {
	c := Config{Root: "/home/u/Acme Stack", Manifest: "/home/u/Acme Stack/multiverse.yaml"}
	c.Normalize()
	e, ok, err := c.Select("")
	if err != nil || !ok {
		t.Fatalf("Select: ok=%v err=%v", ok, err)
	}
	if c.Current != "acme-stack" || e.Root != "/home/u/Acme Stack" {
		t.Fatalf("got current=%q entry=%+v", c.Current, e)
	}
	if c.Root != "" || c.Manifest != "" {
		t.Fatalf("legacy fields survive: %+v", c)
	}
}

func TestSelect(t *testing.T) {
	c := Config{Current: "a", Workspaces: map[string]WorkspaceEntry{"a": {Root: "/a"}, "b": {Root: "/b"}}}
	if e, _, _ := c.Select("b"); e.Root != "/b" {
		t.Fatalf("explicit name ignored: %+v", e)
	}
	if e, _, _ := c.Select(""); e.Root != "/a" {
		t.Fatalf("current ignored: %+v", e)
	}
	if _, _, err := c.Select("zzz"); err == nil {
		t.Fatal("unknown project must fail, not fall back to current")
	}
	if _, ok, err := (Config{}).Select(""); ok || err != nil {
		t.Fatalf("empty config: ok=%v err=%v", ok, err)
	}
}
