package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	Later      []Release // releases newer than the target version, oldest first
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
	if tv, err := ParseVersion(h.Version); err == nil {
		rels, err := gh.ListReleases(ctx, r.Owner, r.Name)
		if err != nil {
			return nil, err
		}
		rp.Later = LaterReleases(rels, tv)
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
	Version  Version
	URL      string // release page
	AssetURL string // download of the program, "" when the release has none
	Size     int64
	Digest   string
}

// UpdateAssetName is the program file attached to every GitHub Relay release.
const UpdateAssetName = "GithubRelay.exe"

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
	u := &UpdateInfo{Version: v, URL: rel.HTMLURL}
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, UpdateAssetName) {
			u.AssetURL, u.Size, u.Digest = a.DownloadURL, a.Size, a.Digest
		}
	}
	return u, nil
}

// Update errors, shown to the user through these text keys.
var (
	errUpdSize   = errors.New("upd.err_size")
	errUpdDigest = errors.New("upd.err_digest")
	errUpdFormat = errors.New("upd.err_format")
)

// DownloadUpdate saves the new program to dst and checks size, SHA-256 and the Windows program header.
// On any problem dst is removed.
func DownloadUpdate(ctx context.Context, client *http.Client, u *UpdateInfo, dst string) (err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.AssetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "GitHub-Relay/"+AppVersion)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &APIError{Status: resp.StatusCode, Path: req.URL.Path}
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(dst)
		}
	}()
	h := sha256.New()
	limit := u.Size
	if limit <= 0 {
		limit = 200 << 20
	}
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if (u.Size > 0 && n != u.Size) || n > limit || n < 1024 {
		return errUpdSize
	}
	if d := strings.TrimPrefix(strings.ToLower(u.Digest), "sha256:"); d != "" && d != hex.EncodeToString(h.Sum(nil)) {
		return errUpdDigest
	}
	if err := f.Close(); err != nil {
		return err
	}
	head := make([]byte, 2)
	if g, err := os.Open(dst); err == nil {
		io.ReadFull(g, head)
		g.Close()
	}
	if string(head) != "MZ" {
		return errUpdFormat
	}
	return nil
}

// SwapProgram puts newFile in place of exe. The running exe is renamed to exe+".old"
// (Windows allows renaming a running program, not overwriting it). On failure the old exe is restored.
func SwapProgram(exe, newFile string) error {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newFile, exe); err != nil {
		os.Rename(old, exe)
		return err
	}
	return nil
}

// ---------- releases around a version ----------

// releaseVersion parses a release tag; ok is false for tags that are not versions.
func releaseVersion(rel Release) (Version, bool) {
	v, err := ParseVersion(rel.TagName)
	return v, err == nil
}

// LaterReleases returns the releases with a version higher than v, oldest first.
func LaterReleases(rels []Release, v Version) []Release {
	var out []Release
	for _, rel := range rels {
		if rv, ok := releaseVersion(rel); ok && rv.Compare(v) > 0 {
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := releaseVersion(out[i])
		b, _ := releaseVersion(out[j])
		return a.Compare(b) < 0
	})
	return out
}

// PreviousRelease returns the highest published release below v, or nil.
func PreviousRelease(rels []Release, v Version) *Release {
	var best *Release
	var bv Version
	for i := range rels {
		rel := rels[i]
		rv, ok := releaseVersion(rel)
		if !ok || rel.Draft || rv.Compare(v) >= 0 {
			continue
		}
		if best == nil || rv.Compare(bv) > 0 {
			best, bv = &rels[i], rv
		}
	}
	return best
}

// DeleteGitHubRelease deletes a release and its tag.
func DeleteGitHubRelease(ctx context.Context, gh *GitHub, r *RepoEntry, rel Release) error {
	if err := gh.DeleteRelease(ctx, r.Owner, r.Name, rel.ID); err != nil && !IsStatus(err, 404) {
		return err
	}
	if rel.TagName != "" {
		return gh.DeleteTag(ctx, r.Owner, r.Name, rel.TagName)
	}
	return nil
}

// MarkRemoved updates the local history after a release was deleted.
func MarkRemoved(r *RepoEntry, rel Release) {
	for _, h := range r.History {
		if (h.ReleaseID != 0 && h.ReleaseID == rel.ID) || (h.ReleaseID == 0 && strings.EqualFold(h.Tag, rel.TagName) && h.Status != StReverted) {
			if h.Status == StSuccess || h.Status == StDraft || h.Status == StExternal {
				h.Status = StRemoved
				h.ReleaseURL = ""
			}
		}
	}
}

// DeleteLaterReleases deletes releases one by one and returns the ones that were deleted.
// It stops at the first error.
func DeleteLaterReleases(ctx context.Context, gh *GitHub, r *RepoEntry, rels []Release) ([]Release, error) {
	var done []Release
	for _, rel := range rels {
		if err := DeleteGitHubRelease(ctx, gh, r, rel); err != nil {
			return done, err
		}
		done = append(done, rel)
	}
	return done, nil
}

// DeletePlan describes deleting one release, and whether it is the latest.
type DeletePlan struct {
	Entry    *HistoryEntry
	IsLatest bool
	Previous *Release    // highest release below the deleted one
	Revert   *RevertPlan // reverting the code to Previous; nil when not latest or nothing would change
}

// PlanDelete checks whether the release is the latest and prepares an optional revert of the code.
func PlanDelete(ctx context.Context, gh *GitHub, r *RepoEntry, h *HistoryEntry) (*DeletePlan, error) {
	dp := &DeletePlan{Entry: h}
	rels, err := gh.ListReleases(ctx, r.Owner, r.Name)
	if err != nil {
		return nil, err
	}
	v, err := ParseVersion(h.Version)
	if err != nil {
		return dp, nil // not a version: just delete it
	}
	dp.IsLatest = len(LaterReleases(rels, v)) == 0
	if !dp.IsLatest {
		return dp, nil
	}
	dp.Previous = PreviousRelease(rels, v)
	if dp.Previous == nil {
		return dp, nil
	}
	pv, _ := releaseVersion(*dp.Previous)
	prev := &HistoryEntry{Version: pv.String(), Tag: dp.Previous.TagName, Status: StExternal}
	rp, err := PlanRevert(ctx, gh, r, prev)
	if err != nil {
		return nil, err
	}
	if !rp.NoChange() {
		dp.Revert = rp
	}
	return dp, nil
}
