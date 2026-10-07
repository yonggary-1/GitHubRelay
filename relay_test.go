package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
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
	if !strings.Contains(SpecMarkdown(LangKO, nil), "추측하지 말고") || !strings.Contains(SpecMarkdown(LangEN, nil), "Do not guess") {
		t.Fatal("no-guess instruction missing")
	}
	if strings.Contains(SpecMarkdown(LangEN, &SpecTarget{Repo: "me/app", Next: "0.1"}), "Do not guess") {
		t.Fatal("no-guess instruction shown with a target")
	}
	// Every text renders without format errors in both languages.
	for k, v := range texts {
		for i, s := range v {
			args := []any{}
			for _, m := range verbRe.FindAllStringSubmatch(s, -1) {
				if m[2] == "d" {
					args = append(args, 1)
				} else {
					args = append(args, "x")
				}
			}
			out := T([]string{LangKO, LangEN}[i], k, args...)
			if strings.Contains(out, "%!") {
				t.Fatalf("%s[%d]: %s", k, i, out)
			}
		}
	}
}
