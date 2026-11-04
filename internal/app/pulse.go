package app

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// UniversePulse is one reading of a universe's resource use, summed over its
// running containers: CPU in cores, memory and network in bytes. A cap of zero
// is no limit set, so the use has no proportion to draw against.
type UniversePulse struct {
	Universe string
	CPU      float64 // cores in use
	// CPUCap is the cores it may use: the containers' limits summed, or the
	// machine's cores when any container has none.
	CPUCap float64
	Mem    float64 // bytes in use
	// MemCap is the bytes it may use, zero when any container has no limit.
	MemCap float64
	Rx     float64 // bytes received since the containers started
	Tx     float64 // bytes sent
}

// ProjectPulse reads every running container of every universe once and sums
// each universe's use, with the caps that bound it: what a fleet tile shows.
func (s *Service) ProjectPulse(ctx context.Context) ([]UniversePulse, error) {
	rows, err := s.ProjectStats(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	caps, err := s.Runtime.ContainerCaps(ctx, names)
	if err != nil {
		return nil, err
	}
	host, err := s.Runtime.HostCapacity(ctx)
	if err != nil {
		return nil, err
	}
	return aggregatePulse(rows, caps, host), nil
}

// aggregatePulse sums each universe's containers into one reading. Used is a
// plain sum; the cap is the sum of the limits, but only while every container
// has one — a single unlimited container makes the CPU ceiling the machine and
// the memory ceiling unknowable (zero), since the rest's limits are no honest
// total to draw against. A sum of limits is capped at the machine's own total,
// which no configuration can let the universe exceed.
func aggregatePulse(rows []StatRow, caps map[string]Caps, host HostCapacity) []UniversePulse {
	type acc struct {
		p                        UniversePulse
		cpuUncapped, memUncapped bool
	}
	by := map[string]*acc{}
	var order []string
	for _, r := range rows {
		a := by[r.Universe]
		if a == nil {
			a = &acc{p: UniversePulse{Universe: r.Universe}}
			by[r.Universe] = a
			order = append(order, r.Universe)
		}
		a.p.CPU += r.CPU / 100 // docker's percent is of one core
		a.p.Mem += r.MemBytes
		a.p.Rx += r.RxBytes
		a.p.Tx += r.TxBytes
		cp := caps[r.Name]
		if cp.CPUs > 0 {
			a.p.CPUCap += cp.CPUs
		} else {
			a.cpuUncapped = true
		}
		if cp.Mem > 0 {
			a.p.MemCap += float64(cp.Mem)
		} else {
			a.memUncapped = true
		}
	}
	out := make([]UniversePulse, 0, len(order))
	for _, name := range order {
		a := by[name]
		if a.cpuUncapped {
			a.p.CPUCap = float64(host.Cores)
		} else if host.Cores > 0 && a.p.CPUCap > float64(host.Cores) {
			a.p.CPUCap = float64(host.Cores)
		}
		if a.memUncapped {
			a.p.MemCap = 0
		} else if host.Mem > 0 && a.p.MemCap > float64(host.Mem) {
			a.p.MemCap = float64(host.Mem)
		}
		out = append(out, a.p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Universe < out[j].Universe })
	return out
}

// UniverseDisk is how much disk a universe holds, split by what it is: Data is
// its docker volumes (its own data, lost on rm), Src the checkout on disk (its
// worktrees, mostly dependencies and build output).
type UniverseDisk struct {
	Data int64
	Src  int64
}

// ProjectDisk is the disk each universe holds, by universe name: its volumes and
// its checkout, measured apart. Both are slow — the volume walk and the tree
// walk — so a caller takes it rarely. A volume is matched to a universe by the
// compose project it is named after.
func (s *Service) ProjectDisk(ctx context.Context) (map[string]UniverseDisk, error) {
	states, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	sizes, err := s.Runtime.VolumeSizes(ctx, s.Manifest.Name)
	if err != nil {
		return nil, err
	}
	// Each app's compose project owns the volumes named after it: "<project>_data".
	project := map[string]string{} // compose project -> universe name
	dirOf := map[string]string{}   // universe name -> its directory of checkouts
	for _, st := range states {
		u := s.UniverseOf(st)
		dirOf[st.Name] = u.Dir()
		for key := range s.Manifest.Apps {
			project[u.ProjectFor(key)] = st.Name
		}
	}

	var mu sync.Mutex
	out := map[string]UniverseDisk{}
	for name, n := range sizes {
		for p, universe := range project {
			if name == p || strings.HasPrefix(name, p+"_") {
				d := out[universe]
				d.Data += n
				out[universe] = d
				break
			}
		}
	}
	// The checkout is a deep tree (node_modules, build output), so walk the
	// universes' directories a few at a time rather than one after another.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for universe, dir := range dirOf {
		if !s.Files.Exists(dir) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(universe, dir string) {
			defer wg.Done()
			defer func() { <-sem }()
			if n, err := s.Files.Size(dir); err == nil {
				mu.Lock()
				d := out[universe]
				d.Src = n
				out[universe] = d
				mu.Unlock()
			}
		}(universe, dir)
	}
	wg.Wait()
	return out, nil
}
