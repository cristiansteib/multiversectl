package compose

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func TestArgv(t *testing.T) {
	r := app.RuntimeRequest{
		Project:  "acme-u1",
		Files:    []string{"a.yml", "b.yml"},
		Profiles: []string{"dev", "workers"},
	}
	got := argv(r, "up", "-d", "web")
	want := []string{"compose", "--project-name", "acme-u1", "-f", "a.yml", "-f", "b.yml",
		"--profile", "dev", "--profile", "workers", "up", "-d", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestLabelValue(t *testing.T) {
	labels := "a=b,com.docker.compose.project=acme-u1-admin,c=d"
	if got := labelValue(labels, projectLabel); got != "acme-u1-admin" {
		t.Fatalf("got %q", got)
	}
	if got := labelValue("a=b", projectLabel); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestPublishedPortsIgnoresExposedOnes(t *testing.T) {
	for ports, want := range map[string]string{
		"5432/tcp":                     "",
		"1025/tcp, 1110/tcp, 8025/tcp": "",
		"":                             "",
		"0.0.0.0:8025->8025/tcp, [::]:8025->8025/tcp": "0.0.0.0:8025->8025/tcp",
		"5432/tcp, 0.0.0.0:8001->8000/tcp":            "0.0.0.0:8001->8000/tcp",
	} {
		if got := publishedPorts(ports); got != want {
			t.Errorf("publishedPorts(%q) = %q, want %q", ports, got, want)
		}
	}
}

// Memory as docker prints it is two sizes in one string, and a page can only
// total or plot it once it is a number of bytes.
func TestMemoryUsageIsReadAsBytes(t *testing.T) {
	for usage, want := range map[string][2]float64{
		"123.4MiB / 7.654GiB": {123.4 * (1 << 20), 7.654 * (1 << 30)},
		"512KiB / 1GiB":       {512 << 10, 1 << 30},
		"0B / 0B":             {0, 0},
		"--":                  {0, 0},
	} {
		used, limit := sizePair(usage)
		if used != want[0] || limit != want[1] {
			t.Errorf("sizePair(%q) = %v, %v, want %v", usage, used, limit, want)
		}
	}
}

func TestStatsLinesAreParsed(t *testing.T) {
	out := []byte(`{"Name":"acme-u3-api-web-1","CPUPerc":"12.34%","MemPerc":"3.10%","MemUsage":"64MiB / 2GiB","NetIO":"1.2kB / 3.4kB","PIDs":"7"}
{"Name":"acme-u3-api-db-1","CPUPerc":"--","MemPerc":"--","MemUsage":"--","NetIO":"--","PIDs":"0"}
`)
	got, err := parseStats(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows", len(got))
	}
	if got[0].CPU != 12.34 || got[0].MemBytes != 64<<20 || got[0].PIDs != 7 {
		t.Errorf("first row = %+v", got[0])
	}
	if got[1].CPU != 0 || got[1].MemBytes != 0 {
		t.Errorf("a container with no reading = %+v", got[1])
	}
}

// It is root deleting recursively: a path near the top of the disk is refused
// before any container is asked.
func TestRemovingAsRootRefusesWhatIsTooCloseToTheTop(t *testing.T) {
	for _, dir := range []string{"", "/", "/home", "/home/dev", "relative/path/here", "."} {
		if err := New(io.Discard, io.Discard, nil).RemoveAsRoot(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "not removing") {
			t.Errorf("%q: %v", dir, err)
		}
	}
}

// The default network comes first, because that is the one the proxy is pointed at
// when a service is on it. A compose file that names its own networks has none.
func TestNetworkKeysPutTheDefaultFirst(t *testing.T) {
	got := networkKeys(map[string]json.RawMessage{"back": nil, "default": nil, "admin": nil})
	want := []string{"default", "admin", "back"}
	if !slices.Equal(got, want) {
		t.Fatalf("networkKeys = %v, want %v", got, want)
	}
	if got := networkKeys(map[string]json.RawMessage{"internal": nil}); !slices.Equal(got, []string{"internal"}) {
		t.Fatalf("networkKeys = %v, want the one network the compose file names", got)
	}
}
