package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ---------- GitHub calls used by release management ----------

// DeleteRelease removes a release (and its attached files) from GitHub.
func (g *GitHub) DeleteRelease(ctx context.Context, owner, name string, id int64) error {
	_, err := g.api(ctx, "DELETE", fmt.Sprintf("%s/releases/%d", repoPath(owner, name), id), nil, nil)
	return err
}

// DeleteTag removes refs/tags/<tag>. A missing tag (e.g. a draft that never had one) is not an error.
func (g *GitHub) DeleteTag(ctx context.Context, owner, name, tag string) error {
	_, err := g.api(ctx, "DELETE", repoPath(owner, name)+"/git/refs/tags/"+escapeRef(tag), nil, nil)
	if IsStatus(err, 404) || IsStatus(err, 422) {
		return nil
	}
	return err
}

// TagCommit resolves a tag to the commit it points to (lightweight or annotated).
func (g *GitHub) TagCommit(ctx context.Context, owner, name, tag string) (string, error) {
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if _, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/ref/tags/"+escapeRef(tag), nil, &ref); err != nil {
		return "", err
	}
	sha, typ := ref.Object.SHA, ref.Object.Type
	for i := 0; i < 5 && typ == "tag"; i++ {
		var t struct {
			Object struct {
				SHA  string `json:"sha"`
				Type string `json:"type"`
			} `json:"object"`
		}
		if _, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/tags/"+sha, nil, &t); err != nil {
			return "", err
		}
		sha, typ = t.Object.SHA, t.Object.Type
	}
	return sha, nil
}

// CommitTreeSHA returns the tree a commit points to.
func (g *GitHub) CommitTreeSHA(ctx context.Context, owner, name, commit string) (string, error) {
	var c struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if _, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/commits/"+commit, nil, &c); err != nil {
		return "", err
	}
	return c.Tree.SHA, nil
}

// LatestRelease reads the newest published release. With an empty token it works for public repositories.
func (g *GitHub) LatestRelease(ctx context.Context, owner, name string) (*Release, error) {
	var rel Release
	if _, err := g.api(ctx, "GET", repoPath(owner, name)+"/releases/latest", nil, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// ---------- delete a release ----------

// CanDeleteRelease: only entries that point at a release on GitHub.
func CanDeleteRelease(h *HistoryEntry) bool {
	return h != nil && h.ReleaseID != 0 && (h.Status == StSuccess || h.Status == StDraft || h.Status == StExternal)
}

// DeleteReleaseEntry deletes the release and its tag. The source commit stays on the branch.
func DeleteReleaseEntry(ctx context.Context, gh *GitHub, r *RepoEntry, h *HistoryEntry) error {
	if err := gh.DeleteRelease(ctx, r.Owner, r.Name, h.ReleaseID); err != nil && !IsStatus(err, 404) {
		return err
	}
	if h.Tag != "" {
		if err := gh.DeleteTag(ctx, r.Owner, r.Name, h.Tag); err != nil {
			return err
		}
	}
	h.Status = StRemoved
	h.ReleaseURL = ""
	return nil
}

// ---------- revert to an earlier version ----------

// RevertPlan describes going back to the files of an earlier release, as a new commit.
type RevertPlan struct {
	Entry      *HistoryEntry
	Branch     string
	HeadSHA    string
	TargetSHA  string // commit of the earlier release
	TargetTree string
	Changes    []Change
	Added      int
	Modified   int
	Deleted    int
}

// CanRevert: entries whose commit is known or can be found through their tag.
func CanRevert(h *HistoryEntry) bool {
	if h == nil {
		return false
	}
	switch h.Status {
	case StSuccess, StDraft, StExternal, StCommitted:
		return h.CommitSHA != "" || h.Tag != ""
	}
	return false
}

// PlanRevert compares the current branch with the files of an earlier release.
func PlanRevert(ctx context.Context, gh *GitHub, r *RepoEntry, h *HistoryEntry) (*RevertPlan, error) {
	info, err := gh.GetRepo(ctx, r.Owner, r.Name)
	if err != nil {
		return nil, err
	}
	rp := &RevertPlan{Entry: h, Branch: r.Branch}
	if rp.Branch == "" {
		rp.Branch = info.DefaultBranch
	}
	if rp.HeadSHA, err = gh.GetBranchHead(ctx, r.Owner, r.Name, rp.Branch); err != nil {
		return nil, err
	}
	rp.TargetSHA = h.CommitSHA
	if rp.TargetSHA == "" {
		if rp.TargetSHA, err = gh.TagCommit(ctx, r.Owner, r.Name, h.Tag); err != nil {
			return nil, err
		}
	}
	if rp.TargetTree, err = gh.CommitTreeSHA(ctx, r.Owner, r.Name, rp.TargetSHA); err != nil {
		return nil, err
	}
	target, tTrunc, err := gh.GetCommitTree(ctx, r.Owner, r.Name, rp.TargetSHA)
	if err != nil {
		return nil, err
	}
	current, cTrunc, err := gh.GetCommitTree(ctx, r.Owner, r.Name, rp.HeadSHA)
	if err != nil {
		return nil, err
	}
	if tTrunc || cTrunc {
		return nil, fmt.Errorf("repository too large to compare")
	}
	tm, cm := blobMap(target), blobMap(current)
	for p, s := range tm {
		if c, ok := cm[p]; !ok {
			rp.Changes = append(rp.Changes, Change{p, ChAdded})
			rp.Added++
		} else if c != s {
			rp.Changes = append(rp.Changes, Change{p, ChModified})
			rp.Modified++
		}
	}
	for p := range cm {
		if _, ok := tm[p]; !ok {
			rp.Changes = append(rp.Changes, Change{p, ChDeleted})
			rp.Deleted++
		}
	}
	sort.Slice(rp.Changes, func(i, j int) bool {
		if rp.Changes[i].Kind != rp.Changes[j].Kind {
			return rp.Changes[i].Kind < rp.Changes[j].Kind
		}
		return rp.Changes[i].Path < rp.Changes[j].Path
	})
	return rp, nil
}

func blobMap(t []TreeEntry) map[string]string {
	m := map[string]string{}
	for _, e := range t {
		if e.Type == "blob" || e.Type == "commit" {
			m[e.Path] = e.Mode + ":" + e.SHA
		}
	}
	return m
}

// NoChange reports a revert that would not change any file.
func (rp *RevertPlan) NoChange() bool { return rp.Added+rp.Modified+rp.Deleted == 0 }

// ExecuteRevert adds a commit on top of the branch whose files equal the earlier release.
// History is never rewritten and nothing is force-pushed.
func ExecuteRevert(ctx context.Context, gh *GitHub, r *RepoEntry, rp *RevertPlan) *HistoryEntry {
	h := &HistoryEntry{
		Time: now(), Version: rp.Entry.Version, Tag: rp.Entry.Tag, Status: StReverted, Branch: rp.Branch,
		Added: rp.Added, Modified: rp.Modified, Deleted: rp.Deleted,
	}
	fail := func(err error) *HistoryEntry {
		k, a := explainAPIError(err)
		h.SetError(k, a...)
		h.Status = StFailed
		return h
	}
	msg := fmt.Sprintf("Revert to %s (restore files of release %s)", strings.TrimSpace(rp.Entry.Tag), rp.Entry.Version)
	commit, curl, err := gh.CreateCommit(ctx, r.Owner, r.Name, msg, rp.TargetTree, rp.HeadSHA)
	if err != nil {
		return fail(err)
	}
	if err := gh.UpdateBranch(ctx, r.Owner, r.Name, rp.Branch, commit); err != nil {
		if IsStatus(err, 422) {
			h.Status = StFailed
			h.SetError("err.moved")
			return h
		}
		return fail(err)
	}
	h.CommitSHA, h.CommitURL = commit, curl
	if h.CommitURL == "" {
		h.CommitURL = fmt.Sprintf("https://github.com/%s/%s/commit/%s", r.Owner, r.Name, commit)
	}
	return h
}

// ---------- update check ----------

// UpdateInfo is a newer GitHub Relay release, if any.
type UpdateInfo struct {
	Version Version
	URL     string
}

// CheckUpdate looks at the project's latest public release. It needs no token.
func CheckUpdate(ctx context.Context, gh *GitHub) (*UpdateInfo, error) {
	owner, name, err := ParseRepo(ProjectURL)
	if err != nil {
		return nil, err
	}
	return checkUpdateFor(ctx, gh, owner, name, AppVersion)
}

func checkUpdateFor(ctx context.Context, gh *GitHub, owner, name, current string) (*UpdateInfo, error) {
	rel, err := gh.LatestRelease(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	v, err := ParseVersion(rel.TagName)
	if err != nil {
		return nil, err
	}
	cur, err := ParseVersion(current)
	if err != nil {
		return nil, err
	}
	if v.Compare(cur) <= 0 {
		return nil, nil
	}
	return &UpdateInfo{Version: v, URL: rel.HTMLURL}, nil
}
