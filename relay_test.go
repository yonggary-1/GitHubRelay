package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type nopProt struct{}

func (nopProt) Protect(b []byte) ([]byte, error)   { return append([]byte("X"), b...), nil }
func (nopProt) Unprotect(b []byte) ([]byte, error) { return b[1:], nil }

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(c))
	}
	zw.Close()
	return buf.Bytes()
}

func manifest(repo, ver string, assets ...string) string {
	a := `[]`
	if len(assets) > 0 {
		a = `["` + strings.Join(assets, `","`) + `"]`
	}
	return `{"spec_version":1,"repo":"` + repo + `","version":"` + ver + `","languages":["en","ko"],"assets":` + a + `}`
}

func goodBundle(repo, ver string) map[string]string {
	return map[string]string{
		"release.json":        manifest(repo, ver, "dist/app.exe"),
		"README.md":           "English | [한국어](README.ko.md)\n\nPurpose.",
		"README.ko.md":        "[English](README.md) | 한국어\n\n목적.",
		"RELEASE_NOTES.md":    "Notes " + ver,
		"RELEASE_NOTES.ko.md": "노트 " + ver,
		"src/main.go":         "package main\n",
		"src/LICENSE":         "MIT",
		"dist/app.exe":        "MZbinary",
	}
}

func levels(cs []Check) (fails []string, warns []string) {
	for _, c := range cs {
		if c.Level == Fail {
			fails = append(fails, c.Key)
		}
		if c.Level == Warn {
			warns = append(warns, c.Key)
		}
	}
	return
}

func TestVersion(t *testing.T) {
	a, _ := ParseVersion("0.1")
	b, _ := ParseVersion("0.1.0")
	c, _ := ParseVersion("0.10")
	d, _ := ParseVersion("v0.9")
	if a.Compare(b) != 0 || c.Compare(d) != 1 || d.Compare(c) != -1 {
		t.Fatal("compare")
	}
	if NextVersion(a).String() != "0.2" {
		t.Fatal("next")
	}
	if _, err := ParseVersion("1.2.3.4"); err == nil {
		t.Fatal("4 parts accepted")
	}
	if _, err := ParseVersion("1"); err == nil {
		t.Fatal("1 part accepted")
	}
}

func TestParseRepo(t *testing.T) {
	for _, s := range []string{"o/r", "https://github.com/o/r", "github.com/o/r.git", "https://www.github.com/o/r/"} {
		o, n, err := ParseRepo(s)
		if err != nil || o != "o" || n != "r" {
			t.Fatalf("%s -> %s %s %v", s, o, n, err)
		}
	}
	for _, s := range []string{"o", "o/r/x", "https://gitlab.com/o/r", "o/..", ""} {
		if _, _, err := ParseRepo(s); err == nil {
			t.Fatalf("%q accepted", s)
		}
	}
}

func TestBundleGood(t *testing.T) {
	b := LoadBundleBytes(makeZip(t, goodBundle("me/app", "0.1")), "Me/App")
	f, _ := levels(b.Checks)
	if len(f) > 0 {
		t.Fatal(f)
	}
	if b.Tag != "v0.1" || len(b.Files) != 6 || len(b.Assets) != 1 {
		t.Fatalf("tag=%s files=%d assets=%d", b.Tag, len(b.Files), len(b.Assets))
	}
	if !strings.Contains(b.ReleaseBody, "Notes 0.1") || !strings.Contains(b.ReleaseBody, "노트 0.1") {
		t.Fatal(b.ReleaseBody)
	}
}

func TestBundleWrapped(t *testing.T) {
	in := map[string]string{}
	for k, v := range goodBundle("me/app", "0.1") {
		in["app-bundle/"+k] = v
	}
	b := LoadBundleBytes(makeZip(t, in), "me/app")
	if f, _ := levels(b.Checks); len(f) > 0 {
		t.Fatal(f)
	}
}

func expectFail(t *testing.T, files map[string]string, key string) {
	t.Helper()
	b := LoadBundleBytes(makeZip(t, files), "me/app")
	f, _ := levels(b.Checks)
	for _, k := range f {
		if k == key {
			return
		}
	}
	t.Fatalf("expected %s, got %v", key, f)
}

func TestBundleFailures(t *testing.T) {
	g := goodBundle("me/app", "0.1")
	m := func(mod func(map[string]string)) map[string]string {
		c := map[string]string{}
		for k, v := range g {
			c[k] = v
		}
		mod(c)
		return c
	}
	expectFail(t, m(func(c map[string]string) { c["../evil.txt"] = "x" }), "chk.unsafe_paths")
	expectFail(t, m(func(c map[string]string) { c["C:/evil.txt"] = "x" }), "chk.unsafe_paths")
	expectFail(t, m(func(c map[string]string) { c["src/a/../../b"] = "x" }), "chk.unsafe_paths")
	expectFail(t, m(func(c map[string]string) { delete(c, "README.ko.md") }), "chk.missing")
	expectFail(t, m(func(c map[string]string) { delete(c, "release.json") }), "chk.missing")
	expectFail(t, m(func(c map[string]string) { c["release.json"] = manifest("other/app", "0.1") }), "chk.repo_mismatch")
	expectFail(t, m(func(c map[string]string) { c["release.json"] = manifest("me/app", "v0.1") }), "chk.version_format")
	expectFail(t, m(func(c map[string]string) { c["release.json"] = manifest("me/app", "0.1", "dist/none.exe") }), "chk.asset_missing")
	expectFail(t, m(func(c map[string]string) {
		c["release.json"] = `{"spec_version":1,"repo":"me/app","version":"0.1","languages":["en","ko"],"extra":1}`
	}), "chk.manifest_json")
	expectFail(t, m(func(c map[string]string) { c["src/config.go"] = "token := \"ghp_" + strings.Repeat("a", 36) + "\"" }), "chk.secret_found")
	expectFail(t, m(func(c map[string]string) {
		c["src/x.txt"] = "github_pat_" + strings.Repeat("B", 22) + "_" + strings.Repeat("C", 59)
	}), "chk.secret_found")
	expectFail(t, m(func(c map[string]string) { c["src/.env"] = "A=1" }), "chk.secret_file")
	expectFail(t, m(func(c map[string]string) { c["src/README.md"] = "dup" }), "chk.doc_in_src")
	expectFail(t, m(func(c map[string]string) { c["src/a.txt"] = "1"; c["src/A.txt"] = "2" }), "chk.case_collision")
	// A token-like string inside a binary is a warning, not a failure.
	bin := LoadBundleBytes(makeZip(t, m(func(c map[string]string) { c["dist/app.exe"] = "MZ\x00\x00ghp_" + strings.Repeat("a", 36) })), "me/app")
	if f, w := levels(bin.Checks); len(f) > 0 || len(w) != 1 {
		t.Fatal(f, w)
	}
	// Packed strings that only resemble a prefix are not secrets.
	pk := LoadBundleBytes(makeZip(t, m(func(c map[string]string) { c["src/a.txt"] = "github_pat_" + strings.Repeat("x", 30) })), "me/app")
	if f, _ := levels(pk.Checks); len(f) > 0 {
		t.Fatal(f)
	}
	// .env.example is fine
	b := LoadBundleBytes(makeZip(t, m(func(c map[string]string) { c["src/.env.example"] = "A=" })), "me/app")
	if f, _ := levels(b.Checks); len(f) > 0 {
		t.Fatal(f)
	}
}

func noProg(int, int, string, ...any) {}

func TestFullFlow(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	r := &RepoEntry{Owner: "me", Name: "app"}

	// Registration on an empty repository.
	reg := VerifyToken(ctx, gh, "me", "app")
	if fl, _ := levels(reg.Checks); len(fl) > 0 || !reg.Empty || reg.State != TokOK {
		t.Fatalf("verify empty: %v %v", fl, reg.State)
	}
	if reg.Expires.Year() != 2099 {
		t.Fatal("expiry not parsed")
	}

	// v0.1 into the empty repo.
	b := LoadBundleBytes(makeZip(t, goodBundle("me/app", "0.1")), "me/app")
	p := Analyze(ctx, gh, r, b)
	if !p.CanUpload() || !p.Empty || p.Added != 6 {
		fl, _ := levels(p.AllChecks())
		t.Fatalf("analyze1 %v empty=%v added=%d", fl, p.Empty, p.Added)
	}
	h := Upload(ctx, gh, p, false, noProg)
	if h.Status != StSuccess {
		t.Fatalf("upload1 %s %s", h.Status, h.ErrorText("en"))
	}
	r.History = append(r.History, h)
	files := f.files()
	if len(files) != 6 || files["main.go"] != "package main\n" || files["README.ko.md"] == "" {
		t.Fatalf("repo files %v", files)
	}
	if string(f.releases[0].Assets["app.exe"]) != "MZbinary" || f.releases[0].Target != h.CommitSHA {
		t.Fatal("release asset/target")
	}

	// Write probe now works (repo not empty).
	if reg := VerifyToken(ctx, gh, "me", "app"); reg.State != TokOK {
		t.Fatal("verify after")
	}

	// Same version again is rejected.
	p = Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, goodBundle("me/app", "0.1")), "me/app"))
	fl, _ := levels(p.Checks)
	if p.CanUpload() || strings.Join(fl, ",") != "chk.tag_exists" {
		t.Fatalf("dup version should give exactly one failure: %v", fl)
	}
	// Lower version is rejected.
	g := goodBundle("me/app", "0.0.9")
	p = Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app"))
	if p.CanUpload() {
		t.Fatal("lower version accepted")
	}

	// v0.2: modify, delete, add workflow.
	g = goodBundle("me/app", "0.2")
	g["src/main.go"] = "package main // v2\n"
	delete(g, "src/LICENSE")
	g["src/.github/workflows/ci.yml"] = "on: push"
	p = Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app"))
	if !p.CanUpload() || p.Modified != 3 || p.Deleted != 1 || p.Added != 1 || !p.NeedsWorkflowApproval() {
		fl, _ := levels(p.AllChecks())
		t.Fatalf("analyze2 %v a=%d m=%d d=%d", fl, p.Added, p.Modified, p.Deleted)
	}
	h2 := Upload(ctx, gh, p, true, noProg)
	if h2.Status != StDraft {
		t.Fatalf("upload2 %s %s", h2.Status, h2.ErrorText("en"))
	}
	r.History = append(r.History, h2)
	files = f.files()
	if _, ok := files["LICENSE"]; ok || files["main.go"] != "package main // v2\n" || files[".github/workflows/ci.yml"] == "" {
		t.Fatalf("repo files after v0.2: %v", files)
	}

	// Branch moved after analysis -> fails safely.
	g = goodBundle("me/app", "0.3")
	p = Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app"))
	g2 := goodBundle("me/app", "0.3.1")
	g2["src/other.txt"] = "x"
	p2 := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g2), "me/app"))
	if h := Upload(ctx, gh, p2, false, noProg); h.Status != StSuccess {
		t.Fatal("p2 upload", h.ErrorText("en"))
	}
	if h := Upload(ctx, gh, p, false, noProg); h.Status != StFailed || h.ErrKey != "err.moved" {
		t.Fatalf("moved branch: %s %s", h.Status, h.ErrKey)
	}

	// Sync: delete a release on "GitHub" and add an external one.
	f.releases = f.releases[1:] // remove v0.1
	if err := Sync(ctx, gh, r); err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	for _, h := range r.History {
		st[h.Tag] = h.Status
	}
	if st["v0.1"] != StDeleted || st["v0.2"] != StDraft || st["v0.3.1"] != StExternal {
		t.Fatalf("sync statuses %v", st)
	}

	// Read-only token: verification reports no write access.
	f.readOnly = true
	if reg := VerifyToken(ctx, gh, "me", "app"); reg.State != TokNoWrite {
		t.Fatalf("readonly state %s", reg.State)
	}
	// Wrong token.
	bad := f.client()
	bad.Token = "nope"
	if reg := VerifyToken(ctx, bad, "me", "app"); reg.State != TokNoAcc {
		t.Fatal("bad token accepted")
	}
}

func TestRetryRelease(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	r := &RepoEntry{Owner: "me", Name: "app"}
	data := makeZip(t, goodBundle("me/app", "0.1"))
	b := LoadBundleBytes(data, "me/app")
	p := Analyze(ctx, gh, r, b)
	// Make release creation fail once by occupying the tag name.
	f.releases = append(f.releases, fakeRelease{Release: Release{ID: 1, TagName: "v0.1"}, Assets: map[string][]byte{}})
	h := Upload(ctx, gh, p, false, noProg)
	if h.Status != StCommitted || h.CommitSHA == "" {
		t.Fatalf("expected committed, got %s %s", h.Status, h.ErrorText("en"))
	}
	f.releases = nil
	RetryRelease(ctx, gh, r, h, b, noProg)
	if h.Status != StSuccess || len(f.releases) != 1 || f.releases[0].Target != h.CommitSHA {
		t.Fatalf("retry: %s %s", h.Status, h.ErrorText("en"))
	}
}

func TestPeekRepo(t *testing.T) {
	dir := t.TempDir()
	in := map[string]string{}
	for k, v := range goodBundle("me/app", "0.1") {
		in["wrap/"+k] = v
	}
	p := filepath.Join(dir, "b.zip")
	os.WriteFile(p, makeZip(t, in), 0o644)
	if r := PeekRepo(p); r != "me/app" {
		t.Fatal(r)
	}
	if PeekRepo(filepath.Join(dir, "none.zip")) != "" {
		t.Fatal("missing file")
	}
}

func TestRename(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	s := &Store{P: nopProt{}}
	r := &RepoEntry{Owner: "me", Name: "app"}
	s.D.Repos = []*RepoEntry{r}
	s.D.LastRepo = "me/app"

	h := Upload(ctx, gh, Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, goodBundle("me/app", "0.1")), "me/app")), false, noProg)
	if h.Status != StSuccess {
		t.Fatal(h.ErrorText("en"))
	}
	r.History = append(r.History, h)

	// Rename on "GitHub".
	f.mu.Lock()
	f.former, f.name = "app", "app2"
	f.mu.Unlock()

	// Analysis under the old name stops and reports the new name.
	p := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, goodBundle("me/app", "0.2")), r.Full(), r.FormerNames...))
	fl, _ := levels(p.Checks)
	if p.RenamedTo != "me/app2" || p.RepoID != 4242 || strings.Join(fl, ",") != "chk.renamed" {
		t.Fatalf("rename not detected: %q %d %v", p.RenamedTo, p.RepoID, fl)
	}
	// Verification under the old name works and continues with the new name (writes too).
	reg := VerifyToken(ctx, gh, "me", "app")
	if reg.State != TokOK || reg.Info.FullName != "me/app2" || reg.Info.ID != 4242 {
		fl, _ := levels(reg.Checks)
		t.Fatalf("verify after rename: %s %v", reg.State, fl)
	}

	// Follow the rename: token and history stay, former name is remembered.
	if err := s.Rename(r, "me/app2"); err != nil {
		t.Fatal(err)
	}
	if r.Full() != "me/app2" || !r.KnownAs("me/app") || s.D.LastRepo != "me/app2" || len(r.History) != 1 {
		t.Fatalf("rename state: %s %v %s", r.Full(), r.FormerNames, s.D.LastRepo)
	}

	// A bundle that still says the old name is accepted with a warning.
	b := LoadBundleBytes(makeZip(t, goodBundle("me/app", "0.2")), r.Full(), r.FormerNames...)
	fl, wl := levels(b.Checks)
	if len(fl) > 0 || strings.Join(wl, ",") != "chk.repo_former" {
		t.Fatalf("former-name bundle: %v %v", fl, wl)
	}
	p = Analyze(ctx, gh, r, b)
	if !p.CanUpload() || !p.NeedsWarnApproval() || p.Latest != "0.1" {
		fl, _ := levels(p.AllChecks())
		t.Fatalf("analyze after rename: %v latest=%s", fl, p.Latest)
	}
	if h := Upload(ctx, gh, p, false, noProg); h.Status != StSuccess {
		t.Fatal(h.ErrorText("en"))
	}
	// An unrelated repository name is still rejected.
	if fl, _ := levels(LoadBundleBytes(makeZip(t, goodBundle("me/other", "0.3")), r.Full(), r.FormerNames...).Checks); len(fl) == 0 {
		t.Fatal("unrelated repo accepted")
	}
	// Renaming back drops the duplicate from the former names.
	s.Rename(r, "me/app")
	if strings.Join(r.FormerNames, ",") != "me/app2" {
		t.Fatal(r.FormerNames)
	}
}

func TestUnrelatedBundle(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	r := &RepoEntry{Owner: "me", Name: "app"}
	g := goodBundle("me/app", "0.1")
	for i := 0; i < 6; i++ {
		g[fmt.Sprintf("src/app%d.go", i)] = "package app"
	}
	if h := Upload(ctx, gh, Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app")), false, noProg); h.Status != StSuccess {
		t.Fatal(h.ErrorText("en"))
	}
	o := goodBundle("me/app", "0.2")
	delete(o, "src/main.go")
	for i := 0; i < 6; i++ {
		o[fmt.Sprintf("src/other%d.py", i)] = "print()"
	}
	p := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, o), "me/app"))
	if fl, _ := levels(p.Checks); p.CanUpload() || !strings.Contains(strings.Join(fl, ","), "chk.unrelated") {
		t.Fatalf("unrelated bundle not blocked: %v", fl)
	}
	u := goodBundle("me/app", "0.2")
	for i := 0; i < 6; i++ {
		u[fmt.Sprintf("src/app%d.go", i)] = "package app // v2"
	}
	u["src/new.go"] = "package app"
	if p := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, u), "me/app")); !p.CanUpload() {
		fl, _ := levels(p.AllChecks())
		t.Fatalf("normal update blocked: %v", fl)
	}
}

func TestStore(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "GithubRelay.dat")
	s, err := LoadStore(p, nopProt{})
	if err != nil {
		t.Fatal(err)
	}
	r := &RepoEntry{Owner: "me", Name: "app", TokenState: TokOK}
	if err := s.SetToken(r, "github_pat_ABCDEFGHIJKLMNOP1234"); err != nil {
		t.Fatal(err)
	}
	s.D.Repos = append(s.D.Repos, r)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte("github_pat_ABCDEFGHIJKLMNOP1234")) {
		t.Fatal("token stored in plain text")
	}
	s2, _ := LoadStore(p, nopProt{})
	tok, err := s2.Token(s2.D.Repos[0])
	if err != nil || tok != "github_pat_ABCDEFGHIJKLMNOP1234" {
		t.Fatal("token roundtrip")
	}
	if r.TokenHint != "github_pat_…1234" {
		t.Fatal(r.TokenHint)
	}
	if st, _ := s2.Status(s2.D.Repos[0], time.Now()); st != TSOk {
		t.Fatal(st)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("temp files left behind")
	}
}

var verbRe = regexp.MustCompile(`%(\[\d+\])?([a-z])`)

func TestSpecAndTexts(t *testing.T) {
	for _, lang := range []string{LangKO, LangEN} {
		s := SpecMarkdown(lang, &SpecTarget{Repo: "me/app", Previous: "0.1", Next: "0.2"})
		if !strings.Contains(s, `"repo": "me/app"`) || !strings.Contains(s, `"version": "0.2"`) {
			t.Fatal("spec missing target")
		}
		if strings.Contains(s, "%!") {
			t.Fatal("format error in spec")
		}
	}
	for _, lang := range []string{LangKO, LangEN} {
		s := SpecMarkdown(lang, nil)
		if !strings.Contains(s, "OWNER/REPO") || strings.Contains(s, "%!") {
			t.Fatal("spec without target")
		}
	}
	if !strings.Contains(SpecMarkdown(LangKO, nil), "**예시**") || !strings.Contains(SpecMarkdown(LangEN, &SpecTarget{Repo: "me/app", Next: "0.1"}), "only for `me/app`") {
		t.Fatal("example / dedicated labels missing")
	}
	if !strings.Contains(SpecMarkdown(LangKO, nil), "추측하지 말고") || !strings.Contains(SpecMarkdown(LangEN, nil), "Do not guess") {
		t.Fatal("no-guess instruction missing")
	}
	if strings.Contains(SpecMarkdown(LangEN, &SpecTarget{Repo: "me/app", Next: "0.1"}), "Do not guess") {
		t.Fatal("no-guess instruction shown with a target")
	}
	// Every text renders without format errors in both languages.
	for k, v := range texts {
		for i, s := range v {
			byPos := map[int]any{}
			pos, maxPos := 0, 0
			for _, m := range verbRe.FindAllStringSubmatch(s, -1) {
				if m[1] != "" {
					pos, _ = strconv.Atoi(strings.Trim(m[1], "[]"))
				} else {
					pos++
				}
				if m[2] == "d" {
					byPos[pos] = 1
				} else {
					byPos[pos] = "x"
				}
				if pos > maxPos {
					maxPos = pos
				}
			}
			args := make([]any, maxPos)
			for p, v := range byPos {
				args[p-1] = v
			}
			out := T([]string{LangKO, LangEN}[i], k, args...)
			if strings.Contains(out, "%!") {
				t.Fatalf("%s[%d]: %s", k, i, out)
			}
		}
	}
}

func TestNaturalOrder(t *testing.T) {
	if !naturalLess("app-v0.9-bundle.zip", "app-v0.10-bundle.zip") || naturalLess("b", "a") {
		t.Fatal("natural order")
	}
}

func TestPreflightBatch(t *testing.T) {
	dir := t.TempDir()
	r := &RepoEntry{Owner: "me", Name: "app"}
	find := func(repo string) *RepoEntry {
		if r.KnownAs(repo) {
			return r
		}
		return nil
	}
	write := func(name, repo, ver string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, makeZip(t, goodBundle(repo, ver)), 0o644)
		return p
	}
	// Named so that plain text order would be wrong (0.10 before 0.9).
	ps := []string{write("app-v0.10.zip", "me/app", "0.10"), write("app-v0.9.zip", "me/app", "0.9"), write("x.zip", "me/app", "0.11")}
	b := PreflightBatch(ps, find)
	if b.Failed() || b.Repo != r {
		t.Fatalf("preflight failed: %v", b.Checks)
	}
	got := []string{}
	for _, it := range b.Items {
		got = append(got, it.Version.String())
	}
	if strings.Join(got, ",") != "0.9,0.10,0.11" || b.NameOrderDiffers {
		t.Fatalf("order %v differs=%v", got, b.NameOrderDiffers)
	}
	// File names that disagree with versions are flagged, versions still win.
	ps2 := []string{write("a.zip", "me/app", "0.3"), write("b.zip", "me/app", "0.2")}
	if b := PreflightBatch(ps2, find); !b.NameOrderDiffers || b.Items[0].Version.String() != "0.2" {
		t.Fatal("name order")
	}
	// Duplicate versions, another repository, unregistered repository.
	if b := PreflightBatch([]string{write("d1.zip", "me/app", "0.5"), write("d2.zip", "me/app", "0.5")}, find); !b.Failed() {
		t.Fatal("duplicate accepted")
	}
	if b := PreflightBatch([]string{write("o1.zip", "me/app", "0.6"), write("o2.zip", "me/other", "0.7")}, find); !b.Failed() {
		t.Fatal("mixed repositories accepted")
	}
	if b := PreflightBatch([]string{write("u.zip", "zz/zz", "0.1")}, find); !b.Failed() || b.Repo != nil {
		t.Fatal("unregistered accepted")
	}
	// A bundle with a secret fails the whole batch before anything is uploaded.
	g := goodBundle("me/app", "0.8")
	g["src/k.txt"] = "ghp_" + strings.Repeat("a", 36)
	sp := filepath.Join(dir, "s.zip")
	os.WriteFile(sp, makeZip(t, g), 0o644)
	if b := PreflightBatch([]string{write("ok.zip", "me/app", "0.7"), sp}, find); !b.Failed() {
		t.Fatal("secret accepted")
	}
}

// TestBatchSequence releases several bundles in order against the fake GitHub, as the UI does.
func TestBatchSequence(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	dir := t.TempDir()
	r := &RepoEntry{Owner: "me", Name: "app"}
	var ps []string
	for i, v := range []string{"0.3", "0.1", "0.2"} {
		g := goodBundle("me/app", v)
		g["src/main.go"] = "package main // " + v
		p := filepath.Join(dir, fmt.Sprintf("b%d.zip", i))
		os.WriteFile(p, makeZip(t, g), 0o644)
		ps = append(ps, p)
	}
	b := PreflightBatch(ps, func(s string) *RepoEntry { return r })
	for b.Next() >= 0 {
		it := b.Items[b.Next()]
		p := Analyze(ctx, gh, r, LoadBundle(it.Path, r.Full()))
		if !p.CanUpload() {
			fl, _ := levels(p.AllChecks())
			t.Fatalf("%s: %v", it.Version, fl)
		}
		h := Upload(ctx, gh, p, false, noProg)
		if h.Status != StSuccess {
			t.Fatal(h.ErrorText("en"))
		}
		r.History = append([]*HistoryEntry{h}, r.History...)
		it.State = BDone
	}
	// Dropping the same set again: every version is already released and is skipped, nothing fails.
	for _, it := range b.Items {
		p := Analyze(ctx, gh, r, LoadBundle(it.Path, r.Full()))
		if !AlreadyReleased(p) {
			fl, _ := levels(p.AllChecks())
			t.Fatalf("re-run %s: %v", it.Version, fl)
		}
	}
	if len(f.releases) != 3 || f.releases[0].TagName != "v0.1" || f.releases[2].TagName != "v0.3" || f.files()["main.go"] != "package main // 0.3" {
		t.Fatalf("releases in wrong order")
	}
}

func TestDeleteAndRevert(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	r := &RepoEntry{Owner: "me", Name: "app"}
	up := func(ver string, mod func(map[string]string)) *HistoryEntry {
		g := goodBundle("me/app", ver)
		if mod != nil {
			mod(g)
		}
		p := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app"))
		if !p.CanUpload() {
			fl, _ := levels(p.AllChecks())
			t.Fatalf("%s: %v", ver, fl)
		}
		h := Upload(ctx, gh, p, false, noProg)
		if h.Status != StSuccess {
			t.Fatal(h.ErrorText("en"))
		}
		r.History = append([]*HistoryEntry{h}, r.History...)
		return h
	}
	h1 := up("0.1", nil)
	h2 := up("0.2", func(g map[string]string) {
		g["src/main.go"] = "package main // broken"
		g["src/extra.go"] = "package main"
		delete(g, "src/LICENSE")
	})

	// Revert to 0.1: one new commit, files equal to 0.1, release 0.2 untouched.
	rp, err := PlanRevert(ctx, gh, r, h1)
	if err != nil {
		t.Fatal(err)
	}
	if rp.Added != 1 || rp.Modified < 1 || rp.Deleted != 1 || rp.NoChange() {
		t.Fatalf("revert plan a=%d m=%d d=%d", rp.Added, rp.Modified, rp.Deleted)
	}
	rh := ExecuteRevert(ctx, gh, r, rp)
	if rh.Status != StReverted || rh.CommitSHA == "" {
		t.Fatal(rh.ErrorText("en"))
	}
	r.History = append([]*HistoryEntry{rh}, r.History...)
	files := f.files()
	if files["main.go"] != "package main\n" || files["LICENSE"] != "MIT" || files["extra.go"] != "" || len(f.releases) != 2 {
		t.Fatalf("files after revert: %v", files)
	}
	if f.commits[rh.CommitSHA][1] != h2.CommitSHA {
		t.Fatal("revert must add a commit on top, not rewrite history")
	}
	// Reverting again to the same state changes nothing.
	if rp, _ := PlanRevert(ctx, gh, r, h1); !rp.NoChange() {
		t.Fatal("second revert should be empty")
	}
	// A revert through the tag only (release made elsewhere).
	ext := &HistoryEntry{Version: "0.2", Tag: "v0.2", Status: StExternal}
	if rp, err := PlanRevert(ctx, gh, r, ext); err != nil || rp.TargetSHA != h2.CommitSHA {
		t.Fatalf("revert by tag: %v", err)
	}

	// Delete release 0.2: release and tag go, the commit stays.
	if !CanDeleteRelease(h2) || CanDeleteRelease(rh) {
		t.Fatal("CanDeleteRelease")
	}
	if err := DeleteReleaseEntry(ctx, gh, r, h2); err != nil {
		t.Fatal(err)
	}
	if h2.Status != StRemoved || len(f.releases) != 1 || f.refs["tags/v0.2"] != "" {
		t.Fatalf("delete: %s releases=%d", h2.Status, len(f.releases))
	}
	if _, ok := f.commits[h2.CommitSHA]; !ok {
		t.Fatal("commit must stay")
	}
	// Sync keeps the removed and reverted entries as they are.
	if err := Sync(ctx, gh, r); err != nil {
		t.Fatal(err)
	}
	if h2.Status != StRemoved || rh.Status != StReverted || h1.Status != StSuccess {
		t.Fatalf("sync changed statuses: %s %s %s", h2.Status, rh.Status, h1.Status)
	}
	// After deleting 0.2, version 0.2 can be released again.
	up("0.2", func(g map[string]string) { g["src/main.go"] = "package main // fixed" })
	if f.files()["main.go"] != "package main // fixed" {
		t.Fatal("re-release")
	}

	// Update check reads the latest public release without a token.
	pub := f.client()
	pub.Token = ""
	rel, err := pub.LatestRelease(ctx, "me", "app")
	if err != nil || rel.TagName != "v0.2" {
		t.Fatalf("latest without token: %v", err)
	}
	if u, err := checkUpdateFor(ctx, pub, "me", "app", "0.1.5"); err != nil || u == nil || u.Version.String() != "0.2" {
		t.Fatalf("update not found: %v", err)
	}
	for _, cur := range []string{"0.2", "0.2.0", "0.10"} {
		if u, err := checkUpdateFor(ctx, pub, "me", "app", cur); err != nil || u != nil {
			t.Fatalf("false update for %s", cur)
		}
	}
}

func TestRevertWithLaterAndDeleteLatest(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	gh := f.client()
	r := &RepoEntry{Owner: "me", Name: "app"}
	hs := map[string]*HistoryEntry{}
	for _, v := range []string{"0.1", "0.2", "0.3", "0.4"} {
		g := goodBundle("me/app", v)
		g["src/main.go"] = "package main // " + v
		p := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app"))
		h := Upload(ctx, gh, p, false, noProg)
		if h.Status != StSuccess {
			t.Fatal(h.ErrorText("en"))
		}
		r.History = append([]*HistoryEntry{h}, r.History...)
		hs[v] = h
	}

	// Deleting a middle release: not latest, no revert offered.
	dp, err := PlanDelete(ctx, gh, r, hs["0.2"])
	if err != nil || dp.IsLatest || dp.Revert != nil {
		t.Fatalf("middle delete plan: %v %+v", err, dp)
	}

	// Deleting the latest release offers reverting the code to the previous release.
	dp, err = PlanDelete(ctx, gh, r, hs["0.4"])
	if err != nil || !dp.IsLatest || dp.Previous == nil || dp.Previous.TagName != "v0.3" || dp.Revert == nil {
		t.Fatalf("latest delete plan: %v %+v", err, dp)
	}
	rh := ExecuteRevert(ctx, gh, r, dp.Revert)
	if rh.Status != StReverted {
		t.Fatal(rh.ErrorText("en"))
	}
	if err := DeleteReleaseEntry(ctx, gh, r, hs["0.4"]); err != nil {
		t.Fatal(err)
	}
	if f.files()["main.go"] != "package main // 0.3" || len(f.releases) != 3 {
		t.Fatalf("after delete latest + revert: %q releases=%d", f.files()["main.go"], len(f.releases))
	}

	// Reverting to 0.1 lists 0.2 and 0.3 as later releases; deleting them leaves 0.1 as latest.
	rp, err := PlanRevert(ctx, gh, r, hs["0.1"])
	if err != nil {
		t.Fatal(err)
	}
	if len(rp.Later) != 2 || rp.Later[0].TagName != "v0.2" || rp.Later[1].TagName != "v0.3" {
		t.Fatalf("later releases: %+v", rp.Later)
	}
	if h := ExecuteRevert(ctx, gh, r, rp); h.Status != StReverted {
		t.Fatal(h.ErrorText("en"))
	}
	done, err := DeleteLaterReleases(ctx, gh, r, rp.Later)
	if err != nil || len(done) != 2 {
		t.Fatalf("delete later: %v", err)
	}
	for _, rel := range done {
		MarkRemoved(r, rel)
	}
	if hs["0.2"].Status != StRemoved || hs["0.3"].Status != StRemoved || hs["0.1"].Status != StSuccess {
		t.Fatal("history not marked")
	}
	if len(f.releases) != 1 || f.files()["main.go"] != "package main // 0.1" {
		t.Fatalf("final state: releases=%d main=%q", len(f.releases), f.files()["main.go"])
	}
	// Now 0.2 can be released again, and the spec suggests 0.2.
	if s := SpecFor(r); s.Next != "0.2" {
		t.Fatalf("spec suggests %s", s.Next)
	}
	g := goodBundle("me/app", "0.2")
	if p := Analyze(ctx, gh, r, LoadBundleBytes(makeZip(t, g), "me/app")); !p.CanUpload() {
		fl, _ := levels(p.AllChecks())
		t.Fatalf("0.2 again: %v", fl)
	}
	// Deleting the only release: latest, but nothing to revert to.
	if dp, err := PlanDelete(ctx, gh, r, hs["0.1"]); err != nil || !dp.IsLatest || dp.Previous != nil || dp.Revert != nil {
		t.Fatalf("only release: %v %+v", err, dp)
	}
}

// TestTextArgs renders texts exactly as the UI calls them, in both languages.
func TestTextArgs(t *testing.T) {
	cases := []struct {
		key  string
		args []any
	}{
		{"del.confirm_latest", []any{"me/app", "0.7", "v0.7", "0.6.1", 1, 2, 3}},
		{"del.confirm", []any{"me/app", "0.7", "v0.7"}},
		{"rev.later_q", []any{"0.3", 2, "v0.4, v0.5"}},
		{"rev.later_only_q", []any{"0.3", 2, "v0.4, v0.5"}},
		{"rev.confirm", []any{"me/app", "0.3", "main", 1, 2, 3, "  x"}},
		{"rev.later_failed", []any{1, 2, "err"}},
		{"batch.confirm", []any{"me/app", 4, "0.1", "0.4", ""}},
		{"confirm.upload", []any{"me/app", "0.1", "main", 1, 2, 3, 1, "x"}},
	}
	for _, c := range cases {
		for _, l := range []string{LangKO, LangEN} {
			s := T(l, c.key, c.args...)
			if strings.Contains(s, "%!") {
				t.Fatalf("%s/%s: %s", c.key, l, s)
			}
		}
	}
	ko := T(LangKO, "del.confirm_latest", "me/app", "0.7", "v0.7", "0.6.1", 1, 2, 3)
	if strings.Count(ko, "0.6.1") != 3 || !strings.Contains(ko, "추가 1 · 수정 2 · 삭제 3") {
		t.Fatal(ko)
	}
}

func TestUpdateDownloadAndSwap(t *testing.T) {
	ctx := context.Background()
	good := append([]byte("MZ"), bytes.Repeat([]byte{0x90}, 4096)...)
	sum := sha256.Sum256(good)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			w.Write(good)
		case "/redirect":
			http.Redirect(w, r, "/good", http.StatusFound)
		case "/text":
			w.Write(bytes.Repeat([]byte("x"), 4098))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	dst := filepath.Join(dir, "new.exe")
	cl := srv.Client()

	if err := DownloadUpdate(ctx, cl, &UpdateInfo{AssetURL: srv.URL + "/redirect", Size: int64(len(good)), Digest: digest}, dst); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		u    UpdateInfo
		want error
	}{
		{UpdateInfo{AssetURL: srv.URL + "/good", Size: int64(len(good)), Digest: "sha256:" + strings.Repeat("0", 64)}, errUpdDigest},
		{UpdateInfo{AssetURL: srv.URL + "/good", Size: 10}, errUpdSize},
		{UpdateInfo{AssetURL: srv.URL + "/text", Size: 4098}, errUpdFormat},
	}
	for _, c := range cases {
		bad := filepath.Join(dir, "bad.exe")
		if err := DownloadUpdate(ctx, cl, &c.u, bad); err != c.want {
			t.Fatalf("want %v, got %v", c.want, err)
		}
		if _, err := os.Stat(bad); err == nil {
			t.Fatal("rejected download left on disk")
		}
	}
	if err := DownloadUpdate(ctx, cl, &UpdateInfo{AssetURL: srv.URL + "/missing"}, filepath.Join(dir, "m.exe")); !IsStatus(err, 404) {
		t.Fatalf("missing asset: %v", err)
	}

	// Swap: old program becomes .old, new one takes its name; data file untouched.
	exe := filepath.Join(dir, "GithubRelay.exe")
	os.WriteFile(exe, []byte("MZold"), 0o755)
	os.WriteFile(filepath.Join(dir, "GithubRelay.dat"), []byte("{}"), 0o644)
	if err := SwapProgram(exe, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, good) {
		t.Fatal("new program not in place")
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "MZold" {
		t.Fatal("old program not kept")
	}
	// A failed swap restores the old program.
	os.Remove(exe + ".old")
	if err := SwapProgram(exe, filepath.Join(dir, "does-not-exist.exe")); err == nil {
		t.Fatal("swap with missing file succeeded")
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, good) {
		t.Fatal("program lost after failed swap")
	}
}

func TestUpdateAssetPick(t *testing.T) {
	ctx := context.Background()
	f := newFake("me", "app")
	defer f.srv.Close()
	f.releases = append(f.releases, fakeRelease{Release: Release{ID: 1, TagName: "v2.0", HTMLURL: "page"}, Assets: map[string][]byte{}})
	f.releases[0].Release.Assets = append(f.releases[0].Release.Assets, struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
		Size        int64  `json:"size"`
		Digest      string `json:"digest"`
	}{Name: "githubrelay.exe", DownloadURL: "dl", Size: 5, Digest: "sha256:ab"})
	pub := f.client()
	pub.Token = ""
	u, err := checkUpdateFor(ctx, pub, "me", "app", "0.9")
	if err != nil || u == nil || u.AssetURL != "dl" || u.Size != 5 || u.Digest != "sha256:ab" || u.URL != "page" {
		t.Fatalf("%v %+v", err, u)
	}
}
