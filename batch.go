package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// Batch item states.
const (
	BWait    = "wait"    // not started
	BRunning = "running" // checking or uploading now
	BDone    = "done"    // released
	BFailed  = "failed"  // stopped here
	BPaused  = "paused"  // waiting for the user (warning not accepted)
	BSkipped = "skipped" // this version was already released on GitHub
)

// BatchItem is one bundle in a batch. Bundle contents are not kept in memory;
// each bundle is read again right before its upload.
type BatchItem struct {
	Path     string
	Name     string
	Repo     string // repo field of release.json
	Version  Version
	Tag      string
	Checks   []Check // local checks from the pre-flight
	State    string
	NoteKey  string // short result: text key and arguments, rendered in the UI language
	NoteArgs []any
}

func (it *BatchItem) SetNote(key string, args ...any) { it.NoteKey, it.NoteArgs = key, args }

func (it *BatchItem) Warnings() []Check {
	var out []Check
	for _, c := range it.Checks {
		if c.Level == Warn {
			out = append(out, c)
		}
	}
	return out
}

// Batch is a set of bundles released one after another to one repository.
type Batch struct {
	Repo             *RepoEntry
	Items            []*BatchItem // in release order (ascending version)
	Checks           []Check      // batch-level problems
	NameOrderDiffers bool         // file-name order differs from version order
}

// Failed reports whether the pre-flight found anything that blocks the batch.
func (b *Batch) Failed() bool {
	if HasFail(b.Checks) {
		return true
	}
	for _, it := range b.Items {
		if HasFail(it.Checks) {
			return true
		}
	}
	return false
}

// Next returns the index of the first item that has not been released, or -1.
func (b *Batch) Next() int {
	for i, it := range b.Items {
		if it.State != BDone && it.State != BSkipped {
			return i
		}
	}
	return -1
}

// Done counts released items.
func (b *Batch) Done() int {
	n := 0
	for _, it := range b.Items {
		if it.State == BDone {
			n++
		}
	}
	return n
}

// PreflightBatch reads every bundle locally, before anything is uploaded.
// find maps a repo name from release.json to a registered repository (current or former name).
func PreflightBatch(paths []string, find func(repo string) *RepoEntry) *Batch {
	b := &Batch{}
	add := func(l Level, k string, a ...any) { b.Checks = append(b.Checks, Check{Level: l, Key: k, Args: a}) }

	// Target repository: all bundles must name the same registered repository.
	for _, p := range paths {
		it := &BatchItem{Path: p, Name: filepath.Base(p), Repo: PeekRepo(p), State: BWait}
		b.Items = append(b.Items, it)
		if b.Repo == nil && it.Repo != "" {
			b.Repo = find(it.Repo)
			if b.Repo == nil {
				add(Fail, "batch.not_registered", it.Repo)
				return b
			}
		}
	}
	if b.Repo == nil {
		add(Fail, "batch.no_repo")
		return b
	}
	for _, it := range b.Items {
		if it.Repo == "" || !b.Repo.KnownAs(it.Repo) {
			it.Checks = append(it.Checks, Check{Level: Fail, Key: "batch.repo_differs", Args: []any{it.Repo, b.Repo.Full()}})
		}
	}

	// Local checks for each bundle.
	for _, it := range b.Items {
		bd := LoadBundle(it.Path, b.Repo.Full(), b.Repo.FormerNames...)
		it.Version, it.Tag = bd.Version, bd.Tag
		for _, c := range bd.Checks {
			if c.Level != Pass && c.Key != "chk.repo_mismatch" { // repo problems are reported once, above
				it.Checks = append(it.Checks, c)
			}
		}
	}

	// Release order: ascending version. Remember the file-name order to compare.
	byName := make([]*BatchItem, len(b.Items))
	copy(byName, b.Items)
	sort.SliceStable(byName, func(i, j int) bool { return naturalLess(byName[i].Name, byName[j].Name) })
	sort.SliceStable(b.Items, func(i, j int) bool { return b.Items[i].Version.Compare(b.Items[j].Version) < 0 })
	for i := range b.Items {
		if b.Items[i] != byName[i] {
			b.NameOrderDiffers = true
		}
	}
	tags := map[string]*BatchItem{}
	for i, it := range b.Items {
		if len(it.Version.Parts) == 0 {
			continue
		}
		if i > 0 && len(b.Items[i-1].Version.Parts) > 0 && b.Items[i-1].Version.Compare(it.Version) == 0 {
			it.Checks = append(it.Checks, Check{Level: Fail, Key: "batch.dup_version", Args: []any{it.Version.String(), b.Items[i-1].Name}})
		}
		if o, ok := tags[strings.ToLower(it.Tag)]; ok && it.Tag != "" {
			it.Checks = append(it.Checks, Check{Level: Fail, Key: "batch.dup_tag", Args: []any{it.Tag, o.Name}})
		}
		tags[strings.ToLower(it.Tag)] = it
	}
	return b
}

// naturalLess compares file names so that "v0.9" sorts before "v0.10".
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		da, db := isDigit(a[0]), isDigit(b[0])
		if da && db {
			na, ra := splitNum(a)
			nb, rb := splitNum(b)
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func splitNum(s string) (string, string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	n := strings.TrimLeft(s[:i], "0")
	return n, s[i:]
}

// AlreadyReleased reports a plan whose only failure is that this version already exists on GitHub.
// Such a bundle is skipped, so a stopped batch can simply be dropped again.
func AlreadyReleased(p *Plan) bool {
	fails := 0
	tag := false
	for _, c := range p.AllChecks() {
		if c.Level == Fail {
			fails++
			if c.Key == "chk.tag_exists" {
				tag = true
			}
		}
	}
	return tag && fails == 1
}
