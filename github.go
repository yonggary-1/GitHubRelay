package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub is a minimal REST client. The token is never included in errors or logs.
type GitHub struct {
	Token     string
	APIBase   string // https://api.github.com
	UploadURL string // https://uploads.github.com (used only when upload_url is absent)
	HTTP      *http.Client
}

func NewGitHub(token string) *GitHub {
	return &GitHub{
		Token:     token,
		APIBase:   "https://api.github.com",
		UploadURL: "https://uploads.github.com",
		HTTP:      &http.Client{Timeout: 15 * time.Minute},
	}
}

// APIError is a non-2xx response.
type APIError struct {
	Status  int
	Message string
	Path    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d (%s)", e.Status, e.Path)
	}
	return fmt.Sprintf("HTTP %d: %s (%s)", e.Status, e.Message, e.Path)
}

func IsStatus(err error, code int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == code
}

type response struct {
	Header http.Header
	Body   []byte
}

func (g *GitHub) do(ctx context.Context, method, fullURL string, body any, contentType string, raw []byte) (*response, error) {
	var rdr io.Reader
	if raw != nil {
		rdr = bytes.NewReader(raw)
	} else if body != nil {
		j, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(j)
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "GitHub-Relay/"+AppVersion)
	req.Header.Set("Authorization", "Bearer "+g.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if raw != nil {
		req.ContentLength = int64(len(raw))
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		// url.Error contains the URL only, never headers.
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	p := req.URL.Path
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &m)
		msg := m.Message
		if strings.Contains(msg, g.Token) && g.Token != "" { // paranoia
			msg = strings.ReplaceAll(msg, g.Token, "***")
		}
		return nil, &APIError{Status: resp.StatusCode, Message: msg, Path: p}
	}
	return &response{Header: resp.Header, Body: data}, nil
}

func (g *GitHub) api(ctx context.Context, method, p string, body any, out any) (*response, error) {
	r, err := g.do(ctx, method, g.APIBase+p, body, "", nil)
	if err != nil {
		return nil, err
	}
	if out != nil && len(r.Body) > 0 {
		if err := json.Unmarshal(r.Body, out); err != nil {
			return nil, fmt.Errorf("decode %s: %w", p, err)
		}
	}
	return r, nil
}

func repoPath(owner, name string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func escapeRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// RepoInfo is the subset of the repository response we use.
type RepoInfo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	Archived      bool   `json:"archived"`
	Permissions   struct {
		Push  bool `json:"push"`
		Admin bool `json:"admin"`
	} `json:"permissions"`
	TokenExpires string `json:"-"`
}

// GetRepo also captures the token expiration header if GitHub sends it.
func (g *GitHub) GetRepo(ctx context.Context, owner, name string) (*RepoInfo, error) {
	var ri RepoInfo
	r, err := g.api(ctx, "GET", repoPath(owner, name), nil, &ri)
	if err != nil {
		return nil, err
	}
	ri.TokenExpires = r.Header.Get("GitHub-Authentication-Token-Expiration")
	return &ri, nil
}

// ParseTokenExpiry parses "2026-11-01 00:00:00 UTC" (and a few variants).
func ParseTokenExpiry(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// IsEmptyRepo reports whether the repository has no commits at all.
func (g *GitHub) IsEmptyRepo(ctx context.Context, owner, name string) (bool, error) {
	_, err := g.api(ctx, "GET", repoPath(owner, name)+"/commits?per_page=1", nil, nil)
	if err == nil {
		return false, nil
	}
	if IsStatus(err, 409) {
		return true, nil
	}
	return false, err
}

// GetBranchHead returns the commit SHA a branch points to.
func (g *GitHub) GetBranchHead(ctx context.Context, owner, name, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	_, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/ref/heads/"+escapeRef(branch), nil, &ref)
	if err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

// TagExists checks refs/tags/<tag>.
func (g *GitHub) TagExists(ctx context.Context, owner, name, tag string) (bool, error) {
	_, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/ref/tags/"+escapeRef(tag), nil, nil)
	if err == nil {
		return true, nil
	}
	if IsStatus(err, 404) {
		return false, nil
	}
	return false, err
}

type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha,omitempty"`
}

// GetCommitTree returns the full recursive tree of a commit.
func (g *GitHub) GetCommitTree(ctx context.Context, owner, name, commitSHA string) ([]TreeEntry, bool, error) {
	var c struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if _, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/commits/"+commitSHA, nil, &c); err != nil {
		return nil, false, err
	}
	var t struct {
		Tree      []TreeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if _, err := g.api(ctx, "GET", repoPath(owner, name)+"/git/trees/"+c.Tree.SHA+"?recursive=1", nil, &t); err != nil {
		return nil, false, err
	}
	return t.Tree, t.Truncated, nil
}

// CreateBlob uploads file content and returns the blob SHA.
func (g *GitHub) CreateBlob(ctx context.Context, owner, name string, data []byte) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	body := map[string]string{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"}
	if _, err := g.api(ctx, "POST", repoPath(owner, name)+"/git/blobs", body, &out); err != nil {
		return "", err
	}
	return out.SHA, nil
}

// CreateTree creates a tree from scratch (no base_tree), so missing files are deleted.
func (g *GitHub) CreateTree(ctx context.Context, owner, name string, entries []TreeEntry) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	if _, err := g.api(ctx, "POST", repoPath(owner, name)+"/git/trees", map[string]any{"tree": entries}, &out); err != nil {
		return "", err
	}
	return out.SHA, nil
}

func (g *GitHub) CreateCommit(ctx context.Context, owner, name, message, tree, parent string) (string, string, error) {
	var out struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
	}
	body := map[string]any{"message": message, "tree": tree, "parents": []string{parent}}
	if _, err := g.api(ctx, "POST", repoPath(owner, name)+"/git/commits", body, &out); err != nil {
		return "", "", err
	}
	return out.SHA, out.HTMLURL, nil
}

// UpdateBranch moves a branch without force; it fails if someone pushed meanwhile.
func (g *GitHub) UpdateBranch(ctx context.Context, owner, name, branch, sha string) error {
	_, err := g.api(ctx, "PATCH", repoPath(owner, name)+"/git/refs/heads/"+escapeRef(branch), map[string]any{"sha": sha, "force": false}, nil)
	return err
}

// CreateFirstFile makes the first commit in an empty repository via the contents API.
func (g *GitHub) CreateFirstFile(ctx context.Context, owner, name, filePath, message string, data []byte) (string, error) {
	var out struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	body := map[string]any{"message": message, "content": base64.StdEncoding.EncodeToString(data)}
	if _, err := g.api(ctx, "PUT", repoPath(owner, name)+"/contents/"+escapeRef(filePath), body, &out); err != nil {
		return "", err
	}
	return out.Commit.SHA, nil
}

type Release struct {
	ID        int64  `json:"id"`
	TagName   string `json:"tag_name"`
	Name      string `json:"name"`
	Draft     bool   `json:"draft"`
	HTMLURL   string `json:"html_url"`
	UploadURL string `json:"upload_url"`
	CreatedAt string `json:"created_at"`
	Published string `json:"published_at"`
}

func (g *GitHub) CreateRelease(ctx context.Context, owner, name, tag, target, title, body string, draft bool) (*Release, error) {
	var rel Release
	req := map[string]any{
		"tag_name": tag, "target_commitish": target, "name": title, "body": body,
		"draft": draft, "prerelease": false,
	}
	if _, err := g.api(ctx, "POST", repoPath(owner, name)+"/releases", req, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// ListReleases returns up to 500 releases, newest first (drafts included for writers).
func (g *GitHub) ListReleases(ctx context.Context, owner, name string) ([]Release, error) {
	var all []Release
	for page := 1; page <= 5; page++ {
		var rs []Release
		if _, err := g.api(ctx, "GET", fmt.Sprintf("%s/releases?per_page=100&page=%d", repoPath(owner, name), page), nil, &rs); err != nil {
			return nil, err
		}
		all = append(all, rs...)
		if len(rs) < 100 {
			break
		}
	}
	return all, nil
}

func (g *GitHub) GetRelease(ctx context.Context, owner, name string, id int64) (*Release, error) {
	var rel Release
	if _, err := g.api(ctx, "GET", fmt.Sprintf("%s/releases/%d", repoPath(owner, name), id), nil, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// UploadAsset attaches a file to a release.
func (g *GitHub) UploadAsset(ctx context.Context, rel *Release, owner, name string, a Asset) error {
	base := rel.UploadURL
	if i := strings.Index(base, "{"); i >= 0 {
		base = base[:i]
	}
	if base == "" {
		base = fmt.Sprintf("%s%s/releases/%d/assets", g.UploadURL, repoPath(owner, name), rel.ID)
	}
	u := base + "?name=" + url.QueryEscape(a.Name)
	_, err := g.do(ctx, "POST", u, nil, "application/octet-stream", a.Data)
	return err
}

// ProbeWrite checks Contents write permission without changing any branch:
// it creates an unreferenced blob, which GitHub garbage-collects later.
func (g *GitHub) ProbeWrite(ctx context.Context, owner, name string) error {
	_, err := g.CreateBlob(ctx, owner, name, []byte("GitHub Relay write check\n"))
	return err
}

type ReleaseAsset struct {
	Name string `json:"name"`
}

func (g *GitHub) ListAssets(ctx context.Context, owner, name string, id int64) ([]ReleaseAsset, error) {
	var out []ReleaseAsset
	if _, err := g.api(ctx, "GET", fmt.Sprintf("%s/releases/%d/assets?per_page=100", repoPath(owner, name), id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
