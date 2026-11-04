package app

import "testing"

// The cap a universe shows is the sum of its containers' limits only while every
// one is limited; a single unlimited container makes CPU fall back to the host's
// cores and memory to no cap at all. Used, and the network totals, are summed
// whatever the caps are.
func TestAggregatePulseSumsUseAndCapsOnlyWhenEveryContainerIsLimited(t *testing.T) {
	row := func(universe, name string, cpuPct, mem, rx, tx float64) StatRow {
		return StatRow{Universe: universe, ContainerStats: ContainerStats{Name: name, CPU: cpuPct, MemBytes: mem, RxBytes: rx, TxBytes: tx}}
	}
	rows := []StatRow{
		// "capped": two containers, both with limits.
		row("capped", "capped-web", 50, 100, 10, 1),
		row("capped", "capped-db", 100, 50, 4, 2),
		// "mixed": one limited, one not.
		row("mixed", "mixed-web", 25, 200, 3, 0),
		row("mixed", "mixed-worker", 75, 300, 0, 0),
		// "oversub": every container limited, but the limits sum past the machine.
		row("oversub", "oversub-a", 10, 100, 0, 0),
		row("oversub", "oversub-b", 10, 100, 0, 0),
	}
	caps := map[string]Caps{
		"capped-web":   {CPUs: 1, Mem: 200},
		"capped-db":    {CPUs: 2, Mem: 300},
		"mixed-web":    {CPUs: 1, Mem: 256},
		"mixed-worker": {}, // no limit
		"oversub-a":    {CPUs: 5, Mem: 10 << 30},
		"oversub-b":    {CPUs: 6, Mem: 8 << 30},
	}
	host := HostCapacity{Cores: 8, Mem: 1 << 34}

	got := aggregatePulse(rows, caps, host)
	if len(got) != 3 {
		t.Fatalf("want 3 universes, got %d", len(got))
	}
	byName := map[string]UniversePulse{}
	for _, p := range got {
		byName[p.Universe] = p
	}

	// Every container limited: use and caps both sum.
	c := byName["capped"]
	if c.CPU != 1.5 { // (50+100)/100 cores
		t.Errorf("capped CPU = %v, want 1.5", c.CPU)
	}
	if c.Mem != 150 {
		t.Errorf("capped Mem = %v, want 150", c.Mem)
	}
	if c.CPUCap != 3 {
		t.Errorf("capped CPUCap = %v, want 3 (1+2)", c.CPUCap)
	}
	if c.MemCap != 500 {
		t.Errorf("capped MemCap = %v, want 500 (200+300)", c.MemCap)
	}
	if c.Rx != 14 || c.Tx != 3 {
		t.Errorf("capped net = %v/%v, want 14/3", c.Rx, c.Tx)
	}

	// One unlimited container: CPU cap is the host's cores, memory has no cap.
	m := byName["mixed"]
	if m.CPU != 1.0 { // (25+75)/100
		t.Errorf("mixed CPU = %v, want 1", m.CPU)
	}
	if m.Mem != 500 {
		t.Errorf("mixed Mem = %v, want 500", m.Mem)
	}
	if m.CPUCap != 8 {
		t.Errorf("mixed CPUCap = %v, want 8 (host cores)", m.CPUCap)
	}
	if m.MemCap != 0 {
		t.Errorf("mixed MemCap = %v, want 0 (unlimited)", m.MemCap)
	}

	// Every container limited but the sum exceeds the host: the cap is the machine.
	o := byName["oversub"]
	if o.CPUCap != 8 {
		t.Errorf("oversub CPUCap = %v, want 8 (clamped to host cores)", o.CPUCap)
	}
	if o.MemCap != float64(host.Mem) {
		t.Errorf("oversub MemCap = %v, want %v (clamped to host mem)", o.MemCap, float64(host.Mem))
	}
}
