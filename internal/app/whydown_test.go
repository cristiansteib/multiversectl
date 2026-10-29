package app

import "testing"

// A service someone stopped exits 143 (SIGTERM) or 137 (SIGKILL after the
// grace period); only another exit is a failure.
func TestAServiceStoppedByHandIsNotAFailure(t *testing.T) {
	for detail, want := range map[string]string{
		"Exited (0) 2 minutes ago":   "stopped",
		"Exited (143) 1 minute ago":  "stopped",
		"Exited (137) 5 seconds ago": "stopped",
		"Exited (1) 2 minutes ago":   "failed",
		"Restarting (1) 3 seconds":   "failed",
	} {
		if got := whyDown(ServiceStatus{Detail: detail}); got != want {
			t.Errorf("%s: %s, want %s", detail, got, want)
		}
	}
}
