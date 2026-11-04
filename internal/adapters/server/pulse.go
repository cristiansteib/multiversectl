package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// How much live history the fleet tiles draw, and how often the slow volume
// sizes are read again. pulseSamples at fleetEvery is about a minute.
const (
	pulseSamples   = 15
	pulseDiskEvery = 60 * time.Second
)

// pulseSample is one universe's current use, as the backend reads it across
// every installed multiverse. Keyed for the ring by multiverse and universe.
type pulseSample struct {
	Multiverse string
	Universe   string
	CPU        float64
	CPUCap     float64
	Mem        float64
	MemCap     float64
	Rx         float64
	Tx         float64
}

func (p pulseSample) key() string { return p.Multiverse + "/" + p.Universe }

// pulseDisk is a universe's disk split by kind (see app.UniverseDisk): Data is
// its volumes, Src its checkout on disk.
type pulseDisk struct{ Data, Src int64 }

// pulseRing keeps, per universe, the latest reading and a short series of CPU
// and memory, plus the slowly-read disk sizes. It is read and written from the
// watch loop and from HTTP handlers, so every method takes the lock.
type pulseRing struct {
	mu   sync.Mutex
	hist map[string]*pulseHist
	disk map[string]pulseDisk
}

type pulseHist struct {
	sample   pulseSample
	cpu, mem []float64
}

func newPulseRing() *pulseRing {
	return &pulseRing{hist: map[string]*pulseHist{}, disk: map[string]pulseDisk{}}
}

// add records this round's samples, forgets universes that are no longer
// running, and returns the view to publish.
func (p *pulseRing) add(samples []pulseSample, at time.Time) pulseView {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := make(map[string]bool, len(samples))
	for _, s := range samples {
		k := s.key()
		seen[k] = true
		h := p.hist[k]
		if h == nil {
			h = &pulseHist{}
			p.hist[k] = h
		}
		h.sample = s
		h.cpu = ringAppend(h.cpu, s.CPU)
		h.mem = ringAppend(h.mem, s.Mem)
	}
	for k := range p.hist {
		if !seen[k] {
			delete(p.hist, k)
		}
	}
	return p.viewLocked(at)
}

func (p *pulseRing) setDisk(disk map[string]pulseDisk) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disk = disk
}

func (p *pulseRing) view(at time.Time) pulseView {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.viewLocked(at)
}

func (p *pulseRing) viewLocked(at time.Time) pulseView {
	v := pulseView{At: at.UTC().Format(time.RFC3339), Universes: []pulseUniverse{}}
	keys := make(map[string]bool, len(p.hist)+len(p.disk))
	for k := range p.hist {
		keys[k] = true
	}
	for k := range p.disk { // a stopped universe has no live sample but still holds volumes
		keys[k] = true
	}
	for k := range keys {
		mv, uni, _ := strings.Cut(k, "/")
		d := p.disk[k]
		pu := pulseUniverse{Multiverse: mv, Universe: uni, Data: d.Data, Src: d.Src, CPUSeries: []float64{}, MemSeries: []float64{}}
		if h := p.hist[k]; h != nil {
			pu.CPU, pu.CPUCap = h.sample.CPU, h.sample.CPUCap
			pu.Mem, pu.MemCap = h.sample.Mem, h.sample.MemCap
			pu.Rx, pu.Tx = h.sample.Rx, h.sample.Tx
			pu.CPUSeries, pu.MemSeries = h.cpu, h.mem
		}
		v.Universes = append(v.Universes, pu)
	}
	sort.Slice(v.Universes, func(i, j int) bool {
		if a, b := v.Universes[i], v.Universes[j]; a.Multiverse != b.Multiverse {
			return a.Multiverse < b.Multiverse
		}
		return v.Universes[i].Universe < v.Universes[j].Universe
	})
	return v
}

func ringAppend(s []float64, v float64) []float64 {
	s = append(s, v)
	if len(s) > pulseSamples {
		s = s[len(s)-pulseSamples:]
	}
	return s
}

// samplePulse takes one live reading, folds it into the ring, and publishes the
// result. Volume sizes are slow, so they are read on their own clock and in the
// background, so a disk pass never holds up the live numbers.
func (s *Server) samplePulse(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	samples, err := s.b.PulseSample(c)
	if err != nil {
		return
	}
	now := time.Now()
	if now.Sub(s.diskAt) >= pulseDiskEvery {
		s.diskAt = now
		go func() {
			dc, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if disk, err := s.b.PulseDisk(dc); err == nil {
				s.pulse.setDisk(disk)
			}
		}()
	}
	s.hub.publish("pulse", event{Type: "pulse", Data: s.pulse.add(samples, now)})
}

// pulseEndpoint serves the current ring over HTTP, for a page that is polling
// because the socket is down; the socket pushes the same view when it is up.
func (s *Server) pulseEndpoint(w http.ResponseWriter, _ *http.Request) {
	reply(w, s.pulse.view(time.Now()))
}
