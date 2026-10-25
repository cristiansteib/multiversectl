package compose

import (
	"reflect"
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
