package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// fakeGitHub is a tiny in-memory GitHub for tests.
type fakeGitHub struct {
	mu       sync.Mutex
	token    string
	readOnly bool
	owner    string
	name     string
	former   string // old repository name that still redirects (GET only)
	branch   string
	blobs    map[string][]byte
	trees    map[string][]TreeEntry
	commits  map[string][2]string // sha -> tree, parent
	refs     map[string]string    // "heads/main" -> sha
	releases []fakeRelease
	srv      *httptest.Server
	nextID   int64
}

type fakeRelease struct {
	Release
	Target string
	Body   string
	Assets map[string][]byte
}

func newFake(owner, name string) *fakeGitHub {
	f := &fakeGitHub{token: "github_pat_TESTTOKEN", owner: owner, name: name, branch: "main",
		blobs: map[string][]byte{}, trees: map[string][]TreeEntry{}, commits: map[string][2]string{}, refs: map[string]string{}, nextID: 100}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeGitHub) client() *GitHub {
	g := NewGitHub(f.token)
	g.APIBase = f.srv.URL
	g.UploadURL = f.srv.URL
	return g
}

func sha(s string) string { h := sha1.Sum([]byte(s)); return hex.EncodeToString(h[:]) }

func (f *fakeGitHub) jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (f *fakeGitHub) errOut(w http.ResponseWriter, code int, msg string) {
	f.jsonOut(w, code, map[string]string{"message": msg})
}

// files returns path->content at the head of the branch.
func (f *fakeGitHub) files() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	head, ok := f.refs["heads/"+f.branch]
	if !ok {
		return out
	}
	for _, e := range f.trees[f.commits[head][0]] {
		out[e.Path] = string(f.blobs[e.SHA])
	}
	return out
}

func (f *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		f.errOut(w, 401, "Bad credentials")
		return
	}
	if f.former != "" {
		op := "/repos/" + f.owner + "/" + f.former
		if r.URL.Path == op || strings.HasPrefix(r.URL.Path, op+"/") {
			if r.Method != "GET" {
				// Writes through an old name must never be relied on.
				f.errOut(w, 404, "Not Found (old name)")
				return
			}
			u := "/repos/" + f.owner + "/" + f.name + strings.TrimPrefix(r.URL.Path, op)
			if r.URL.RawQuery != "" {
				u += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, u, http.StatusMovedPermanently)
			return
		}
	}
	prefix := "/repos/" + f.owner + "/" + f.name
	p := r.URL.Path
	if !strings.HasPrefix(p, prefix) {
		f.errOut(w, 404, "Not Found")
		return
	}
	p = strings.TrimPrefix(p, prefix)
	body, _ := io.ReadAll(r.Body)
	write := r.Method != "GET"
	if write && f.readOnly {
		f.errOut(w, 403, "Resource not accessible by personal access token")
		return
	}
	empty := len(f.refs) == 0
	switch {
	case p == "" && r.Method == "GET":
		w.Header().Set("GitHub-Authentication-Token-Expiration", "2099-01-01 00:00:00 UTC")
		f.jsonOut(w, 200, map[string]any{"id": 4242, "full_name": f.owner + "/" + f.name, "default_branch": f.branch, "html_url": "https://github.com/x"})
	case p == "/commits":
		if empty {
			f.errOut(w, 409, "Git Repository is empty.")
			return
		}
		f.jsonOut(w, 200, []any{map[string]string{"sha": f.refs["heads/"+f.branch]}})
	case strings.HasPrefix(p, "/git/ref/"):
		ref := strings.TrimPrefix(p, "/git/ref/")
		if empty {
			f.errOut(w, 409, "Git Repository is empty.")
			return
		}
		s, ok := f.refs[ref]
		if !ok {
			f.errOut(w, 404, "Not Found")
			return
		}
		f.jsonOut(w, 200, map[string]any{"object": map[string]string{"sha": s}})
	case p == "/git/blobs" && r.Method == "POST":
		if empty {
			f.errOut(w, 409, "Git Repository is empty.")
			return
		}
		var in struct{ Content, Encoding string }
		json.Unmarshal(body, &in)
		data, _ := base64.StdEncoding.DecodeString(in.Content)
		s := GitBlobSHA(data)
		f.blobs[s] = data
		f.jsonOut(w, 201, map[string]string{"sha": s})
	case p == "/git/trees" && r.Method == "POST":
		var in struct{ Tree []TreeEntry }
		json.Unmarshal(body, &in)
		for _, e := range in.Tree {
			if _, ok := f.blobs[e.SHA]; !ok {
				f.errOut(w, 422, "missing blob "+e.Path)
				return
			}
		}
		sort.Slice(in.Tree, func(i, j int) bool { return in.Tree[i].Path < in.Tree[j].Path })
		j, _ := json.Marshal(in.Tree)
		s := sha(string(j))
		f.trees[s] = in.Tree
		f.jsonOut(w, 201, map[string]string{"sha": s})
	case strings.HasPrefix(p, "/git/trees/"):
		s := strings.TrimPrefix(p, "/git/trees/")
		f.jsonOut(w, 200, map[string]any{"tree": f.trees[s], "truncated": false})
	case p == "/git/commits" && r.Method == "POST":
		var in struct {
			Message, Tree string
			Parents       []string
		}
		json.Unmarshal(body, &in)
		s := sha(in.Message + in.Tree + strings.Join(in.Parents, ","))
		f.commits[s] = [2]string{in.Tree, in.Parents[0]}
		f.jsonOut(w, 201, map[string]string{"sha": s, "html_url": "https://github.com/c/" + s})
	case strings.HasPrefix(p, "/git/commits/"):
		s := strings.TrimPrefix(p, "/git/commits/")
		c, ok := f.commits[s]
		if !ok {
			f.errOut(w, 404, "Not Found")
			return
		}
		f.jsonOut(w, 200, map[string]any{"tree": map[string]string{"sha": c[0]}})
	case strings.HasPrefix(p, "/git/refs/") && r.Method == "PATCH":
		ref := strings.TrimPrefix(p, "/git/refs/")
		var in struct {
			SHA   string
			Force bool
		}
		json.Unmarshal(body, &in)
		cur := f.refs[ref]
		if f.commits[in.SHA][1] != cur && !in.Force {
			f.errOut(w, 422, "Update is not a fast forward")
			return
		}
		f.refs[ref] = in.SHA
		f.jsonOut(w, 200, map[string]any{})
	case strings.HasPrefix(p, "/contents/") && r.Method == "PUT":
		path := strings.TrimPrefix(p, "/contents/")
		var in struct{ Message, Content string }
		json.Unmarshal(body, &in)
		data, _ := base64.StdEncoding.DecodeString(in.Content)
		bs := GitBlobSHA(data)
		f.blobs[bs] = data
		tree := []TreeEntry{{Path: path, Mode: "100644", Type: "blob", SHA: bs}}
		j, _ := json.Marshal(tree)
		ts := sha(string(j))
		f.trees[ts] = tree
		cs := sha("init" + ts)
		f.commits[cs] = [2]string{ts, ""}
		f.refs["heads/"+f.branch] = cs
		f.jsonOut(w, 201, map[string]any{"commit": map[string]string{"sha": cs}})
	case p == "/releases" && r.Method == "GET":
		out := []Release{}
		for i := len(f.releases) - 1; i >= 0; i-- {
			out = append(out, f.releases[i].Release)
		}
		f.jsonOut(w, 200, out)
	case p == "/releases" && r.Method == "POST":
		var in struct {
			TagName         string `json:"tag_name"`
			TargetCommitish string `json:"target_commitish"`
			Name, Body      string
			Draft           bool
		}
		json.Unmarshal(body, &in)
		for _, x := range f.releases {
			if x.TagName == in.TagName {
				f.errOut(w, 422, "already_exists")
				return
			}
		}
		f.nextID++
		rel := fakeRelease{Release: Release{ID: f.nextID, TagName: in.TagName, Name: in.Name, Draft: in.Draft,
			HTMLURL:   fmt.Sprintf("https://github.com/r/%d", f.nextID),
			UploadURL: fmt.Sprintf("%s%s/releases/%d/assets{?name,label}", f.srv.URL, prefix, f.nextID),
			CreatedAt: "2026-10-07T00:00:00Z"}, Target: in.TargetCommitish, Body: in.Body, Assets: map[string][]byte{}}
		if !in.Draft {
			f.refs["tags/"+in.TagName] = in.TargetCommitish
		}
		f.releases = append(f.releases, rel)
		f.jsonOut(w, 201, rel.Release)
	case strings.HasPrefix(p, "/releases/"):
		rest := strings.Split(strings.TrimPrefix(p, "/releases/"), "/")
		id, _ := strconv.ParseInt(rest[0], 10, 64)
		var rel *fakeRelease
		for i := range f.releases {
			if f.releases[i].ID == id {
				rel = &f.releases[i]
			}
		}
		if rel == nil {
			f.errOut(w, 404, "Not Found")
			return
		}
		if len(rest) == 1 {
			f.jsonOut(w, 200, rel.Release)
			return
		}
		if r.Method == "GET" {
			out := []ReleaseAsset{}
			for n := range rel.Assets {
				out = append(out, ReleaseAsset{Name: n})
			}
			f.jsonOut(w, 200, out)
			return
		}
		rel.Assets[r.URL.Query().Get("name")] = body
		f.jsonOut(w, 201, map[string]any{})
	default:
		f.errOut(w, 404, "Not Found: "+r.Method+" "+p)
	}
}
