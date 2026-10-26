package server

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Letting another machine in
//
// The link carries a 192-bit token, which is not something to read out over a
// desk or type on a phone. So whoever already has it can ask the running
// server for a pairing code — six digits, good for a few minutes — and
// whoever types that into the sign-in page is given the same cookie the link
// would have given them.
//
// Six digits is a million, and a machine on the same network could work
// through a million of anything in very little time. What keeps that from
// mattering is that there is usually nothing to guess at: no code exists until
// someone holding the token asks for one, and the one that exists is spent by
// the first use, dies after a few wrong guesses, and is forgotten minutes
// later. An attacker gets those few guesses out of a million, and only while
// somebody is in the middle of letting a second machine in.
const (
	pairLife  = 5 * time.Minute
	pairTries = 5
)

type pairing struct {
	mu    sync.Mutex
	code  string
	until time.Time
	left  int
}

// offer makes a code and forgets whichever one was waiting: asking again
// because the first was mistyped should not leave two ways in.
func (p *pairing) offer(now time.Time) (string, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code = pairCode()
	p.until = now.Add(pairLife)
	p.left = pairTries
	return p.code, p.until
}

// claim spends the code. Why it failed is not said: the person typing knows
// what they typed, and whoever is guessing learns nothing.
func (p *pairing) claim(given string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" || now.After(p.until) || p.left <= 0 {
		p.code = ""
		return false
	}
	p.left--
	if subtle.ConstantTimeCompare([]byte(p.code), []byte(digitsOf(given))) != 1 {
		if p.left <= 0 {
			p.code = ""
		}
		return false
	}
	p.code = ""
	return true
}

// pairCode is six digits, read as two groups of three.
func pairCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic("server: no randomness: " + err.Error())
	}
	return fmt.Sprintf("%06d", n)
}

// digitsOf keeps only the digits, so a code written down as "418 902" or
// "418-902" is the code that was given.
func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pairNew hands out a code. It is behind the guard like everything else, so
// only a request that already carries the token reaches it.
func (s *Server) pairNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	code, until := s.pair.offer(time.Now())
	reply(w, map[string]any{
		"code":    code,
		"expires": until.UTC().Format(time.RFC3339),
		"at":      ReachableAt(s.o.Addr),
		"exposed": s.o.Expose,
	})
}

// pairUse is the one thing a request without the cookie may do, so it is
// handled in the guard rather than the mux. It answers the sign-in page's
// form, which is a form and not a script because the page it is on is served
// before anything of this server's has been allowed to load.
func (s *Server) pairUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.signIn(w, http.StatusBadRequest, "That was not a code.")
		return
	}
	if !s.pair.claim(r.PostFormValue("code"), time.Now()) {
		s.signIn(w, http.StatusUnauthorized, "That code is not the one, or it has been used, or it has run out. Ask for another.")
		return
	}
	setSession(w, s.o.Token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ReachableAt is where another machine on the same network would open this
// server: every address this machine answers on, with the port it listens on.
// A server bound to one address is only at that one.
func ReachableAt(listen string) []string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
		return []string{"http://" + net.JoinHostPort(host, port) + "/"}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	// The one the machine would use to reach anything, which on a developer's
	// machine is the one address among a dozen docker bridges that another
	// machine can actually get to. It goes first, and is the only one worth
	// reading out; the rest are there for the network it does not describe.
	best := outwardIP()
	var out []string
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		// A loopback address is no use to the machine being let in, and an
		// address this one has not settled on yet is no use to anyone.
		if !ok || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() || n.IP.To4() == nil {
			continue
		}
		at := "http://" + net.JoinHostPort(n.IP.String(), port) + "/"
		if n.IP.Equal(best) {
			out = append([]string{at}, out...)
			continue
		}
		out = append(out, at)
	}
	return out
}

// outwardIP is the address this machine would send from, found by asking the
// routing table for a route and not by sending anything: a UDP socket is
// connected without a packet leaving, and the address it was given is the one
// the default route goes out of. Nil when there is no route at all.
func outwardIP() net.IP {
	c, err := net.Dial("udp", "203.0.113.1:9")
	if err != nil {
		return nil
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP
	}
	return nil
}
