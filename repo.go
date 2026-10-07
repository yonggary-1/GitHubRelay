package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ParseRepo accepts "owner/name", "github.com/owner/name",
// "https://github.com/owner/name(.git)(/)" and returns owner and name.
func ParseRepo(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(low, p) {
			s = s[len(p):]
			low = low[len(p):]
		}
	}
	if strings.HasPrefix(low, "www.") {
		s, low = s[4:], low[4:]
	}
	if strings.HasPrefix(low, "github.com/") {
		s = s[len("github.com/"):]
	}
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) != 2 || !ownerRe.MatchString(parts[0]) || !nameRe.MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." {
		return "", "", fmt.Errorf("invalid repository %q", s)
	}
	return parts[0], parts[1], nil
}

// SameRepo compares two "owner/name" strings case-insensitively (GitHub names are case-insensitive).
func SameRepo(a, b string) bool {
	ao, an, err1 := ParseRepo(a)
	bo, bn, err2 := ParseRepo(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ao, bo) && strings.EqualFold(an, bn)
}
