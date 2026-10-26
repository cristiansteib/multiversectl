package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestACodeOpensOneMachineOnce(t *testing.T) {
	var p pairing
	now := time.Now()
	code, until := p.offer(now)
	if len(code) != 6 {
		t.Fatalf("want six digits, got %q", code)
	}
	if !until.After(now) {
		t.Fatalf("want it to last a while, got %s", until.Sub(now))
	}
	// Written down with a space, as it is read out.
	if !p.claim(code[:3]+" "+code[3:], now) {
		t.Fatal("the code it gave out was refused")
	}
	if p.claim(code, now) {
		t.Error("a code that let one machine in let a second one in too")
	}
}

func TestACodeRunsOutOfGuesses(t *testing.T) {
	var p pairing
	now := time.Now()
	code, _ := p.offer(now)
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	for i := 0; i < pairTries; i++ {
		if p.claim(wrong, now) {
			t.Fatalf("guess %d was accepted", i+1)
		}
	}
	if p.claim(code, now) {
		t.Error("the right code still worked after every guess was used up")
	}
}

func TestACodeIsForgotten(t *testing.T) {
	var p pairing
	now := time.Now()
	code, _ := p.offer(now)
	if p.claim(code, now.Add(pairLife+time.Second)) {
		t.Error("a code from five minutes ago was accepted")
	}
}

func TestAskingAgainLeavesOneWayIn(t *testing.T) {
	var p pairing
	now := time.Now()
	first, _ := p.offer(now)
	second, _ := p.offer(now)
	if first == second {
		t.Skip("the same six digits twice; nothing to tell apart")
	}
	if p.claim(first, now) {
		t.Error("the code that was replaced still worked")
	}
	if !p.claim(second, now) {
		t.Error("the code that replaced it did not")
	}
}

// The sign-in page is what a machine without the cookie is shown, and the one
// thing it may do is say a code.
func TestAMachineWithoutTheLinkIsOfferedTheForm(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	rec := do(t, h, "GET", "/", "", map[string]string{"Cookie": ""})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("= %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`action="` + pairPath + `"`, "multiversectl server pair", `href="/style.css"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the sign-in page does not offer %q", want)
		}
	}
	// Its own policy lets it post here, where the rest of the server posts nowhere.
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self'") {
		t.Errorf("the form cannot be sent under %q", csp)
	}
	// And the stylesheet it names is readable without signing in.
	if css := do(t, h, "GET", "/style.css", "", map[string]string{"Cookie": ""}); css.Code != http.StatusOK {
		t.Errorf("the sign-in page's stylesheet is behind the sign-in: %d", css.Code)
	}
}

func TestTypingTheCodeLetsTheMachineIn(t *testing.T) {
	s, h := serve(t, &fake{}, Options{})
	code, _ := s.pair.offer(time.Now())

	wrong := do(t, h, "POST", pairPath, "code=000000", map[string]string{
		"Cookie": "", "Content-Type": "application/x-www-form-urlencoded"})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong code = %d", wrong.Code)
	}
	if strings.Contains(wrong.Body.String(), code) {
		t.Error("the page being guessed at said what the code was")
	}

	right := do(t, h, "POST", pairPath, "code="+url.QueryEscape(code), map[string]string{
		"Cookie": "", "Content-Type": "application/x-www-form-urlencoded"})
	if right.Code != http.StatusSeeOther {
		t.Fatalf("the right code = %d %s", right.Code, right.Body)
	}
	var session *http.Cookie
	for _, c := range right.Result().Cookies() {
		if c.Name == cookieName {
			session = c
		}
	}
	if session == nil || session.Value != testToken {
		t.Fatalf("want the session cookie, got %+v", session)
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Errorf("the cookie a code gives out is weaker than the one the link gives: %+v", session)
	}
}

// Read-only is about what the page may do, not about who may look at it.
func TestAReadOnlyServerStillLetsAMachineIn(t *testing.T) {
	s, h := serve(t, &fake{}, Options{ReadOnly: true})
	code, _ := s.pair.offer(time.Now())
	rec := do(t, h, "POST", pairPath, "code="+code, map[string]string{
		"Cookie": "", "Content-Type": "application/x-www-form-urlencoded"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
}

func TestOnlySomebodyWhoIsInCanAskForACode(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	out := do(t, h, "POST", "/api/pair", "", map[string]string{"Cookie": "", csrfHeader: csrfValue})
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("= %d %s", out.Code, out.Body)
	}
	in := do(t, h, "POST", "/api/pair", "", map[string]string{csrfHeader: csrfValue})
	if in.Code != http.StatusOK {
		t.Fatalf("= %d %s", in.Code, in.Body)
	}
	if !strings.Contains(in.Body.String(), `"code":"`) {
		t.Errorf("no code in %s", in.Body)
	}
}

func TestWhereAnotherMachineWouldOpenIt(t *testing.T) {
	// Bound to one address: that is the only one it answers on.
	if at := ReachableAt("192.168.1.23:7777"); len(at) != 1 || at[0] != "http://192.168.1.23:7777/" {
		t.Errorf("want the address it is bound to, got %v", at)
	}
	// Bound to everything: whatever this machine has, and never loopback.
	for _, at := range ReachableAt("0.0.0.0:7777") {
		if strings.Contains(at, "127.0.0.1") || strings.Contains(at, "[::1]") {
			t.Errorf("%s is no use to another machine", at)
		}
		if !strings.HasSuffix(at, ":7777/") {
			t.Errorf("%s is not on the port it listens on", at)
		}
	}
	if at := ReachableAt("not an address"); at != nil {
		t.Errorf("want nothing for something that is not an address, got %v", at)
	}
}

// The browser decides whether to send the Origin header by the referrer
// policy of the page the form is on, and the rest of this server says
// no-referrer — under which the form arrives from nowhere and the guard
// refuses it as cross-site.
func TestTheSignInPageCanSendItsOwnForm(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	rec := do(t, h, "GET", "/", "", map[string]string{"Cookie": ""})
	if got := rec.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Errorf("Referrer-Policy %q: the form will arrive with Origin: null and be refused", got)
	}
}
