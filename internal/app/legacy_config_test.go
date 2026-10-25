package app

import "testing"

func TestAConfigThatSaysWorkspacesIsReadAsProjects(t *testing.T) {
	c := Config{LegacyWorkspaces: map[string]WorkspaceEntry{"old": {Root: "/r/old"}, "both": {Root: "/r/legacy"}},
		Workspaces: map[string]WorkspaceEntry{"both": {Root: "/r/new"}, "new": {Root: "/r/new"}}}
	c.Normalize()
	if c.LegacyWorkspaces != nil {
		t.Error("nothing writes the old key any more")
	}
	if c.Workspaces["old"].Root != "/r/old" || len(c.Workspaces) != 3 {
		t.Errorf("projects = %+v", c.Workspaces)
	}
	if c.Workspaces["both"].Root != "/r/new" {
		t.Errorf("the entry already under the new name wins: %+v", c.Workspaces["both"])
	}
}
