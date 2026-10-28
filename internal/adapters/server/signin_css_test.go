package server

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Nothing on the sign-in page is set smaller than 1rem: small print was the
// first thing anybody said about the page, and one `font: 12px` brings it back.
func TestNoTextIsSmallerThanOneRem(t *testing.T) {
	decl := regexp.MustCompile(`(?:^|[;{\s])font(?:-size)?:[^;}]*`)
	size := regexp.MustCompile(`(\d+(?:\.\d+)?)(px|em|rem)\b`)
	for _, d := range decl.FindAllString(string(signInCSS), -1) {
		for _, m := range size.FindAllStringSubmatch(d, -1) {
			n, _ := strconv.ParseFloat(m[1], 64)
			if (m[2] == "px" && n < 16) || (m[2] != "px" && n < 1) {
				t.Errorf("text smaller than 1rem: %s", strings.TrimSpace(d))
			}
		}
	}
}
