package release

import (
	"reflect"
	"testing"
)

func TestReleaseNotesAreCountedAsFixesAndOtherChanges(t *testing.T) {
	body := `## [0.0.42](https://github.com/acme/tool/compare/v0.0.41...v0.0.42) (2026-10-27)

### Features

* **cli:** a thing ([#40](https://github.com/acme/tool/issues/40)) ([1a2b3c4](https://github.com/acme/tool/commit/1a2b3c4))
  * detail of the thing, which is not another change

### Bug Fixes

* a repository with only a compose file runs as itself ([#44](https://github.com/acme/tool/issues/44)) ([8f2a2aa](https://github.com/acme/tool/commit/8f2a2aa))
* **ui:** the page parses ([#49](https://github.com/acme/tool/issues/49)) ([d5c309f](https://github.com/acme/tool/commit/d5c309f))
    * nor is this
`
	fixes, changes := parseNotes(body)
	want := []string{"a repository with only a compose file runs as itself", "the page parses"}
	if !reflect.DeepEqual(fixes, want) || changes != 1 {
		t.Errorf("got %q and %d changes", fixes, changes)
	}
}
