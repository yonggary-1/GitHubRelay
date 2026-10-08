package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// AppVersion is the version of GitHub Relay itself.
const AppVersion = "0.7"

// ProjectURL is GitHub Relay's own repository. It is only shown to the user
// (About dialog); uploads always go to repositories the user registers.
const ProjectURL = "https://github.com/yonggary-1/GitHubRelay"

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// Version is a release version with two or three numeric parts (0.1 or 0.1.1).
type Version struct {
	Parts []int
}

// ParseVersion accepts "0.1", "0.1.1", and tolerates a leading "v".
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if !versionRe.MatchString(s) {
		return Version{}, fmt.Errorf("invalid version %q", s)
	}
	var v Version
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, fmt.Errorf("invalid version %q", s)
		}
		v.Parts = append(v.Parts, n)
	}
	return v, nil
}

// Compare returns -1, 0, 1. Missing parts count as 0, so 0.1 == 0.1.0.
func (a Version) Compare(b Version) int {
	for i := 0; i < 3; i++ {
		x, y := 0, 0
		if i < len(a.Parts) {
			x = a.Parts[i]
		}
		if i < len(b.Parts) {
			y = b.Parts[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func (a Version) String() string {
	s := make([]string, len(a.Parts))
	for i, p := range a.Parts {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ".")
}

// NextVersion suggests the next version with the same number of parts:
// 0.1 -> 0.2, 0.1.1 -> 0.1.2.
func NextVersion(a Version) Version {
	n := Version{Parts: append([]int(nil), a.Parts...)}
	n.Parts[len(n.Parts)-1]++
	return n
}

// MaxVersion returns the highest parseable version from tags, and whether any was found.
func MaxVersion(tags []string) (Version, bool) {
	var best Version
	found := false
	for _, t := range tags {
		v, err := ParseVersion(t)
		if err != nil {
			continue
		}
		if !found || v.Compare(best) > 0 {
			best, found = v, true
		}
	}
	return best, found
}
