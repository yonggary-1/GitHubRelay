package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ChangeKind int

const (
	ChAdded ChangeKind = iota
	ChModified
	ChDeleted
)

type Change struct {
	Path string
	Kind ChangeKind
}

// Plan is the result of comparing a bundle with the repository.
type Plan struct {
	Bundle    *Bundle
	Owner     string
	Name      string
	Branch    string
	Empty     bool
	HeadSHA   string
	Existing  map[string]TreeEntry
	Changes   []Change
	Added     int
	Modified  int
	Deleted   int
	Unchanged int
	Checks    []Check // remote checks
	Latest    string  // highest released version on GitHub, "" if none
	RepoID    int64   // GitHub repository id
	RenamedTo string  // set when GitHub reports a different current owner/name
	RepoURL   string
}

func (p *Plan) add(l Level, key string, args ...any) {
	p.Checks = append(p.Checks, Check{Level: l, Key: key, Args: args})
}

// AllChecks returns local then remote checks.
func (p *Plan) AllChecks() []Check {
	return append(append([]Check(nil), p.Bundle.Checks...), p.Checks...)
}

func (p *Plan) NeedsWorkflowApproval() bool {
	for _, c := range p.Checks {
		if c.Level == Warn && c.Kind == WarnWorkflow {
			return true
		}
	}
	return false
}

func (p *Plan) NeedsWarnApproval() bool {
	for _, c := range p.AllChecks() {
		if c.Level == Warn && c.Kind == WarnGeneric {
			return true
		}
	}
	return false
}

// CanUpload: no failures anywhere.
func (p *Plan) CanUpload() bool { return !HasFail(p.AllChecks()) }

func explainAPIError(err error) (string, []any) {
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case 401:
			return "err.401", nil
		case 403:
			return "err.403", []any{ae.Message}
		case 404:
			return "err.404", nil
		case 409:
			return "err.409", []any{ae.Message}
		case 422:
			return "err.422", []any{ae.Message}
		}
		return "err.api", []any{ae.Error()}
	}
	return "err.network", []any{err.Error()}
}

// Analyze performs the remote checks and computes the change list.
func Analyze(ctx context.Context, gh *GitHub, r *RepoEntry, b *Bundle) *Plan {
	p := &Plan{Bundle: b, Owner: r.Owner, Name: r.Name, RepoURL: r.URL(), Existing: map[string]TreeEntry{}}
	if b.Manifest.SpecVersion == 0 && len(b.Files) == 0 {
		return p // nothing usable to compare
	}
	info, err := gh.GetRepo(ctx, r.Owner, r.Name)
	if err != nil {
		k, a := explainAPIError(err)
		p.add(Fail, k, a...)
		return p
	}
	p.RepoID = info.ID
	if info.FullName != "" && !SameRepo(info.FullName, r.Full()) {
		// The repository was renamed or transferred; GitHub answered through a redirect.
		p.RenamedTo = info.FullName
		p.add(Fail, "chk.renamed", r.Full(), info.FullName)
		return p
	}
	if info.Archived {
		p.add(Fail, "chk.archived")
		return p
	}
	p.Branch = r.Branch
	if p.Branch == "" {
		p.Branch = info.DefaultBranch
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	empty, err := gh.IsEmptyRepo(ctx, r.Owner, r.Name)
	if err != nil {
		k, a := explainAPIError(err)
		p.add(Fail, k, a...)
		return p
	}
	p.Empty = empty
	if empty {
		if r.Branch != "" && info.DefaultBranch != "" && r.Branch != info.DefaultBranch {
			p.add(Fail, "chk.empty_branch", r.Branch, info.DefaultBranch)
			return p
		}
		p.add(Pass, "chk.empty_repo", p.Branch)
	} else {
		head, err := gh.GetBranchHead(ctx, r.Owner, r.Name, p.Branch)
		if err != nil {
			if IsStatus(err, 404) {
				p.add(Fail, "chk.branch_missing", p.Branch)
			} else {
				k, a := explainAPIError(err)
				p.add(Fail, k, a...)
			}
			return p
		}
		p.HeadSHA = head
		tree, truncated, err := gh.GetCommitTree(ctx, r.Owner, r.Name, head)
		if err != nil {
			k, a := explainAPIError(err)
			p.add(Fail, k, a...)
			return p
		}
		if truncated {
			p.add(Fail, "chk.tree_truncated")
			return p
		}
		for _, e := range tree {
			if e.Type == "blob" || e.Type == "commit" {
				p.Existing[e.Path] = e
			}
		}
		p.add(Pass, "chk.branch_ok", p.Branch)
	}

	// Version and tag checks against GitHub (drafts have no tag yet, so check releases too).
	if b.Tag != "" {
		rels, err := gh.ListReleases(ctx, r.Owner, r.Name)
		if err != nil {
			k, a := explainAPIError(err)
			p.add(Fail, k, a...)
			return p
		}
		tags := []string{}
		dup := false
		for _, rel := range rels {
			tags = append(tags, rel.TagName)
			if strings.EqualFold(rel.TagName, b.Tag) {
				dup = true
			}
		}
		if !dup && !empty {
			exists, err := gh.TagExists(ctx, r.Owner, r.Name, b.Tag)
			if err != nil {
				k, a := explainAPIError(err)
				p.add(Fail, k, a...)
				return p
			}
			dup = exists
		}
		// Compare only with releases that exist on GitHub.
		if dup {
			p.add(Fail, "chk.tag_exists", b.Tag)
		} else if latest, ok := MaxVersion(tags); ok {
			p.Latest = latest.String()
			if len(b.Version.Parts) > 0 && b.Version.Compare(latest) <= 0 {
				p.add(Fail, "chk.version_not_newer", b.Version.String(), latest.String())
			} else {
				p.add(Pass, "chk.version_ok", b.Version.String(), latest.String())
			}
		} else if len(b.Version.Parts) > 0 {
			p.add(Pass, "chk.version_first", b.Version.String())
		}
	}

	// Change list. The bundle is a full snapshot of the repository.
	inBundle := map[string]bool{}
	for _, f := range b.Files {
		inBundle[f.Path] = true
		e, ok := p.Existing[f.Path]
		switch {
		case !ok:
			p.Changes = append(p.Changes, Change{f.Path, ChAdded})
			p.Added++
		case e.Type != "blob" || e.SHA != f.SHA:
			p.Changes = append(p.Changes, Change{f.Path, ChModified})
			p.Modified++
		default:
			p.Unchanged++
		}
	}
	for path := range p.Existing {
		if !inBundle[path] {
			p.Changes = append(p.Changes, Change{path, ChDeleted})
			p.Deleted++
		}
	}
	sort.Slice(p.Changes, func(i, j int) bool {
		if p.Changes[i].Kind != p.Changes[j].Kind {
			return p.Changes[i].Kind < p.Changes[j].Kind
		}
		return p.Changes[i].Path < p.Changes[j].Path
	})
	// A bundle for another project shares (almost) no files with this repository.
	if !empty {
		core, common := 0, 0
		for path := range p.Existing {
			if isBoilerplate(path) {
				continue
			}
			core++
			if inBundle[path] {
				common++
			}
		}
		if core >= 5 && float64(common) < float64(core)*unrelatedRatio {
			p.add(Fail, "chk.unrelated", common, core)
		}
	}
	if p.Added+p.Modified+p.Deleted == 0 {
		p.add(Warn, "chk.no_changes")
	}

	wf := []string{}
	for _, c := range p.Changes {
		if c.Kind != ChDeleted && IsWorkflowPath(c.Path) {
			wf = append(wf, c.Path)
		}
	}
	if len(wf) > 0 {
		p.Checks = append(p.Checks, Check{Level: Warn, Kind: WarnWorkflow, Key: "chk.workflow", Args: []any{strings.Join(limitList(wf, 5), ", ")}})
	}
	if n := len(p.Existing); n >= 10 && float64(p.Deleted)/float64(n) > deleteWarnRatio {
		p.add(Warn, "chk.many_deletes", p.Deleted, n)
	}
	return p
}

// Progress reports upload steps to the UI.
type Progress func(done, total int, key string, args ...any)

func now() string { return time.Now().Format(time.RFC3339) }

// Upload performs the release. It never force-pushes.
func Upload(ctx context.Context, gh *GitHub, p *Plan, draft bool, prog Progress) *HistoryEntry {
	b := p.Bundle
	h := &HistoryEntry{
		Time: now(), Version: b.Version.String(), Tag: b.Tag, Title: b.Title, Branch: p.Branch,
		Added: p.Added, Modified: p.Modified, Deleted: p.Deleted, Draft: draft, BundlePath: b.FilePath,
	}
	need := p.Added + p.Modified
	total := need + 4 + len(b.Assets)
	if p.Empty {
		total++
	}
	step := 0
	fail := func(err error) *HistoryEntry {
		k, a := explainAPIError(err)
		h.SetError(k, a...)
		if h.CommitSHA != "" {
			h.Status = StCommitted
		} else {
			h.Status = StFailed
		}
		return h
	}
	head := p.HeadSHA
	existing := p.Existing
	if p.Empty {
		prog(step, total, "up.init")
		sha, err := gh.CreateFirstFile(ctx, p.Owner, p.Name, "README.md", "Initialize repository", b.ReadmeMain)
		if err != nil {
			return fail(err)
		}
		head = sha
		existing = map[string]TreeEntry{"README.md": {Path: "README.md", Mode: "100644", Type: "blob", SHA: GitBlobSHA(b.ReadmeMain)}}
		step++
	}
	shas := map[string]string{}
	changed := map[string]bool{}
	for _, c := range p.Changes {
		if c.Kind != ChDeleted {
			changed[c.Path] = true
		}
	}
	for _, f := range b.Files {
		if !changed[f.Path] {
			shas[f.Path] = f.SHA
			continue
		}
		if e, ok := existing[f.Path]; ok && e.SHA == f.SHA {
			shas[f.Path] = f.SHA // e.g. README.md created by the init commit
			step++
			continue
		}
		prog(step, total, "up.blob", f.Path)
		sha, err := gh.CreateBlob(ctx, p.Owner, p.Name, f.Data)
		if err != nil {
			return fail(err)
		}
		shas[f.Path] = sha
		step++
	}
	prog(step, total, "up.tree")
	entries := make([]TreeEntry, 0, len(b.Files))
	for _, f := range b.Files {
		mode := "100644"
		if e, ok := existing[f.Path]; ok && e.Mode == "100755" {
			mode = "100755"
		}
		entries = append(entries, TreeEntry{Path: f.Path, Mode: mode, Type: "blob", SHA: shas[f.Path]})
	}
	tree, err := gh.CreateTree(ctx, p.Owner, p.Name, entries)
	if err != nil {
		return fail(err)
	}
	step++
	prog(step, total, "up.commit")
	commit, curl, err := gh.CreateCommit(ctx, p.Owner, p.Name, b.CommitMsg, tree, head)
	if err != nil {
		return fail(err)
	}
	step++
	prog(step, total, "up.ref", p.Branch)
	if err := gh.UpdateBranch(ctx, p.Owner, p.Name, p.Branch, commit); err != nil {
		if IsStatus(err, 422) {
			h.Status = StFailed
			h.SetError("err.moved")
			return h
		}
		return fail(err)
	}
	h.CommitSHA, h.CommitURL = commit, curl
	if h.CommitURL == "" {
		h.CommitURL = fmt.Sprintf("https://github.com/%s/%s/commit/%s", p.Owner, p.Name, commit)
	}
	step++
	return finishRelease(ctx, gh, p.Owner, p.Name, b, h, draft, step, total, prog)
}

func finishRelease(ctx context.Context, gh *GitHub, owner, name string, b *Bundle, h *HistoryEntry, draft bool, step, total int, prog Progress) *HistoryEntry {
	fail := func(err error) *HistoryEntry {
		k, a := explainAPIError(err)
		h.SetError(k, a...)
		h.Status = StCommitted
		return h
	}
	var rel *Release
	var err error
	if h.ReleaseID != 0 {
		rel, err = gh.GetRelease(ctx, owner, name, h.ReleaseID)
		if err != nil && !IsStatus(err, 404) {
			return fail(err)
		}
	}
	if rel == nil {
		prog(step, total, "up.release", b.Tag)
		rel, err = gh.CreateRelease(ctx, owner, name, b.Tag, h.CommitSHA, b.Title, b.ReleaseBody, draft)
		if err != nil {
			return fail(err)
		}
		h.ReleaseID, h.ReleaseURL = rel.ID, rel.HTMLURL
	}
	step++
	have := map[string]bool{}
	if as, err := gh.ListAssets(ctx, owner, name, rel.ID); err == nil {
		for _, a := range as {
			have[strings.ToLower(a.Name)] = true
		}
	}
	for _, a := range b.Assets {
		if have[strings.ToLower(a.Name)] {
			step++
			continue
		}
		prog(step, total, "up.asset", a.Name)
		if err := gh.UploadAsset(ctx, rel, owner, name, a); err != nil {
			return fail(err)
		}
		step++
	}
	prog(total, total, "up.done")
	h.SetError("")
	h.Draft = rel.Draft
	if rel.Draft {
		h.Status = StDraft
	} else {
		h.Status = StSuccess
	}
	return h
}

// RetryRelease finishes a release whose commit already landed.
func RetryRelease(ctx context.Context, gh *GitHub, r *RepoEntry, h *HistoryEntry, b *Bundle, prog Progress) {
	if HasFail(b.Checks) {
		h.SetError("err.bundle_changed")
		return
	}
	if b.Tag != h.Tag {
		h.SetError("err.bundle_changed")
		return
	}
	res := finishRelease(ctx, gh, r.Owner, r.Name, b, h, h.Draft, 0, 1+len(b.Assets), prog)
	*h = *res
}

// Sync reconciles local history with the releases on GitHub.
func Sync(ctx context.Context, gh *GitHub, r *RepoEntry) error {
	rels, err := gh.ListReleases(ctx, r.Owner, r.Name)
	if err != nil {
		return err
	}
	byTag := map[string]Release{}
	byID := map[int64]Release{}
	for _, rel := range rels {
		byTag[strings.ToLower(rel.TagName)] = rel
		byID[rel.ID] = rel
	}
	known := map[string]bool{}
	for _, h := range r.History {
		if h.Status == StFailed {
			continue
		}
		rel, ok := byID[h.ReleaseID]
		if !ok {
			rel, ok = byTag[strings.ToLower(h.Tag)]
		}
		if !ok {
			if h.Status == StSuccess || h.Status == StDraft || h.Status == StExternal {
				h.Status = StDeleted
			}
			continue
		}
		known[strings.ToLower(rel.TagName)] = true
		h.ReleaseID, h.ReleaseURL, h.Draft = rel.ID, rel.HTMLURL, rel.Draft
		if h.Status == StSuccess || h.Status == StDraft || h.Status == StDeleted {
			if rel.Draft {
				h.Status = StDraft
			} else {
				h.Status = StSuccess
			}
		}
	}
	for _, rel := range rels {
		if known[strings.ToLower(rel.TagName)] {
			continue
		}
		t := rel.Published
		if t == "" {
			t = rel.CreatedAt
		}
		v := strings.TrimPrefix(strings.TrimPrefix(rel.TagName, "v"), "V")
		r.History = append(r.History, &HistoryEntry{
			Time: t, Version: v, Tag: rel.TagName, Title: rel.Name, Status: StExternal,
			ReleaseID: rel.ID, ReleaseURL: rel.HTMLURL, Draft: rel.Draft,
		})
	}
	sort.SliceStable(r.History, func(i, j int) bool { return r.History[i].Time > r.History[j].Time })
	return nil
}

// RegisterResult is the outcome of verifying a token for a repository.
type RegisterResult struct {
	Info    *RepoInfo
	State   string
	Expires time.Time
	Empty   bool
	Checks  []Check
}

// VerifyToken checks access, write permission, expiry and emptiness.
func VerifyToken(ctx context.Context, gh *GitHub, owner, name string) *RegisterResult {
	res := &RegisterResult{State: TokUnknown}
	add := func(l Level, k string, a ...any) { res.Checks = append(res.Checks, Check{Level: l, Key: k, Args: a}) }
	info, err := gh.GetRepo(ctx, owner, name)
	if err != nil {
		k, a := explainAPIError(err)
		add(Fail, k, a...)
		res.State = TokNoAcc
		return res
	}
	res.Info = info
	add(Pass, "reg.access_ok", info.FullName)
	// After a rename, continue with the current name instead of relying on redirects.
	if o, n, err := ParseRepo(info.FullName); err == nil {
		owner, name = o, n
	}
	if t, ok := ParseTokenExpiry(info.TokenExpires); ok {
		res.Expires = t
		if time.Until(t) < 0 {
			add(Fail, "reg.expired", t.Local().Format("2006-01-02"))
		} else if time.Until(t) < 14*24*time.Hour {
			add(Warn, "reg.expiring", t.Local().Format("2006-01-02"))
		} else {
			add(Pass, "reg.expires", t.Local().Format("2006-01-02"))
		}
	} else {
		add(Warn, "reg.no_expiry")
	}
	empty, err := gh.IsEmptyRepo(ctx, owner, name)
	if err != nil {
		k, a := explainAPIError(err)
		add(Fail, k, a...)
		return res
	}
	res.Empty = empty
	if empty {
		add(Warn, "reg.empty_write_unknown")
		res.State = TokOK
		return res
	}
	if err := gh.ProbeWrite(ctx, owner, name); err != nil {
		if IsStatus(err, 403) || IsStatus(err, 404) {
			add(Fail, "reg.no_write")
			res.State = TokNoWrite
			return res
		}
		k, a := explainAPIError(err)
		add(Fail, k, a...)
		return res
	}
	add(Pass, "reg.write_ok")
	res.State = TokOK
	return res
}

// unrelatedRatio: below this share of common files, a bundle is treated as another project's.
const unrelatedRatio = 0.2

// isBoilerplate reports root files that almost every project has, which say nothing about identity.
func isBoilerplate(p string) bool {
	if strings.Contains(p, "/") {
		return false
	}
	l := strings.ToLower(p)
	for _, pre := range []string{"readme", "release_notes", "license", "licence", "changelog", ".gitignore", ".gitattributes", ".editorconfig"} {
		if strings.HasPrefix(l, pre) {
			return true
		}
	}
	return false
}
