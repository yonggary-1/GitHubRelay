package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// SpecVersion is the bundle specification version this program understands.
const SpecVersion = 1

const (
	maxBundleTotal  = 1 << 30           // 1 GiB uncompressed in total
	maxSourceFile   = 100 * 1000 * 1000 // GitHub hard limit for a single file
	warnSourceFile  = 50 * 1000 * 1000  // GitHub warns above 50 MB
	maxAssetFile    = 2 << 30           // GitHub release asset limit (2 GiB)
	maxBundleFiles  = 20000
	deleteWarnRatio = 0.30
)

type Level int

const (
	Pass Level = iota
	Warn
	Fail
)

// WarnKind separates warnings that need their own approval checkbox.
type WarnKind int

const (
	WarnGeneric WarnKind = iota
	WarnWorkflow
)

// Check is one validation result. Text is rendered later in the UI language.
type Check struct {
	Level Level
	Kind  WarnKind
	Key   string
	Args  []any
}

func (c Check) Text(lang string) string { return T(lang, c.Key, c.Args...) }

// Manifest mirrors release.json.
type Manifest struct {
	SpecVersion   int      `json:"spec_version"`
	Repo          string   `json:"repo"`
	Version       string   `json:"version"`
	Tag           string   `json:"tag"`
	Title         string   `json:"title"`
	CommitMessage string   `json:"commit_message"`
	Languages     []string `json:"languages"`
	Assets        []string `json:"assets"`
}

// RepoFile is a file that will exist in the repository after upload.
type RepoFile struct {
	Path string
	Data []byte
	SHA  string // git blob SHA-1
}

// Asset is a file attached to the release.
type Asset struct {
	Name string
	Data []byte
}

// Bundle is a parsed and locally validated bundle.
type Bundle struct {
	FilePath    string
	Manifest    Manifest
	Version     Version
	Tag         string
	Title       string
	CommitMsg   string
	Languages   []string
	Files       []RepoFile // sorted by path
	Assets      []Asset
	ReleaseBody string
	ReadmeMain  []byte
	Checks      []Check
}

func (b *Bundle) add(l Level, key string, args ...any) {
	b.Checks = append(b.Checks, Check{Level: l, Key: key, Args: args})
}

func (b *Bundle) addKind(l Level, k WarnKind, key string, args ...any) {
	b.Checks = append(b.Checks, Check{Level: l, Kind: k, Key: key, Args: args})
}

// HasFail reports whether any check failed.
func HasFail(cs []Check) bool {
	for _, c := range cs {
		if c.Level == Fail {
			return true
		}
	}
	return false
}

// GitBlobSHA computes the SHA-1 git uses for a blob with this content.
func GitBlobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

var (
	langRe  = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
	tagRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	driveRe = regexp.MustCompile(`^[A-Za-z]:`)
)

// cleanZipName normalizes a zip entry name and reports whether it is safe.
func cleanZipName(name string) (string, bool) {
	n := strings.ReplaceAll(name, "\\", "/")
	if n == "" || strings.HasPrefix(n, "/") || driveRe.MatchString(n) || strings.ContainsRune(n, 0) {
		return n, false
	}
	for _, seg := range strings.Split(strings.TrimSuffix(n, "/"), "/") {
		if seg == ".." {
			return n, false
		}
	}
	c := path.Clean(n)
	if c == "." || strings.HasPrefix(c, "../") {
		return n, false
	}
	if strings.HasSuffix(n, "/") {
		return c + "/", true
	}
	return c, true
}

// LoadBundle reads a bundle zip fully in memory (never extracting to disk) and runs local checks.
// expectRepo is "owner/name" of the repository the user selected.
func LoadBundle(zipPath string, expectRepo string) *Bundle {
	b := &Bundle{FilePath: zipPath}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		b.add(Fail, "chk.zip_open", err.Error())
		return b
	}
	defer zr.Close()
	files, ok := readZip(b, &zr.Reader)
	if !ok {
		return b
	}
	validate(b, files, expectRepo)
	return b
}

// LoadBundleBytes is LoadBundle for in-memory data (used by tests).
func LoadBundleBytes(data []byte, expectRepo string) *Bundle {
	b := &Bundle{FilePath: "(memory)"}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.add(Fail, "chk.zip_open", err.Error())
		return b
	}
	files, ok := readZip(b, zr)
	if !ok {
		return b
	}
	validate(b, files, expectRepo)
	return b
}

func readZip(b *Bundle, zr *zip.Reader) (map[string][]byte, bool) {
	files := map[string][]byte{}
	var total uint64
	bad := []string{}
	count := 0
	for _, f := range zr.File {
		name, safe := cleanZipName(f.Name)
		if !safe {
			bad = append(bad, f.Name)
			continue
		}
		if strings.HasSuffix(name, "/") || f.FileInfo().IsDir() {
			continue
		}
		if f.Mode()&0o170000 == 0o120000 { // symlink entry
			bad = append(bad, f.Name)
			continue
		}
		count++
		if count > maxBundleFiles {
			b.add(Fail, "chk.too_many_files", maxBundleFiles)
			return nil, false
		}
		total += f.UncompressedSize64
		if total > maxBundleTotal {
			b.add(Fail, "chk.too_large")
			return nil, false
		}
		rc, err := f.Open()
		if err != nil {
			b.add(Fail, "chk.zip_read", f.Name, err.Error())
			return nil, false
		}
		// Limit actual bytes read so a lying header cannot blow memory up.
		data, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		if err != nil {
			b.add(Fail, "chk.zip_read", f.Name, err.Error())
			return nil, false
		}
		if uint64(len(data)) > f.UncompressedSize64 {
			b.add(Fail, "chk.zip_read", f.Name, "size mismatch")
			return nil, false
		}
		if _, dup := files[name]; dup {
			b.add(Fail, "chk.duplicate_entry", name)
			return nil, false
		}
		files[name] = data
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		b.add(Fail, "chk.unsafe_paths", strings.Join(limitList(bad, 5), ", "))
		return nil, false
	}
	b.add(Pass, "chk.paths_ok")
	// A zip made by compressing a folder often has a single top-level folder. Unwrap it.
	if _, ok := files["release.json"]; !ok {
		if prefix := singleTopFolder(files); prefix != "" {
			if _, ok := files[prefix+"release.json"]; ok {
				nf := make(map[string][]byte, len(files))
				for k, v := range files {
					nf[strings.TrimPrefix(k, prefix)] = v
				}
				files = nf
				b.add(Pass, "chk.unwrapped", strings.TrimSuffix(prefix, "/"))
			}
		}
	}
	return files, true
}

func singleTopFolder(files map[string][]byte) string {
	top := ""
	for k := range files {
		i := strings.Index(k, "/")
		if i < 0 {
			return ""
		}
		t := k[:i+1]
		if top == "" {
			top = t
		} else if top != t {
			return ""
		}
	}
	return top
}

func limitList(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	out := append([]string(nil), s[:n]...)
	return append(out, fmt.Sprintf("… (+%d)", len(s)-n))
}

func isRootDoc(name string, langs []string) bool {
	for i, l := range langs {
		for _, base := range []string{"README", "RELEASE_NOTES"} {
			if i == 0 && name == base+".md" {
				return true
			}
			if i > 0 && name == base+"."+l+".md" {
				return true
			}
		}
	}
	return false
}

func docName(base string, langs []string, i int) string {
	if i == 0 {
		return base + ".md"
	}
	return base + "." + langs[i] + ".md"
}

func validate(b *Bundle, files map[string][]byte, expectRepo string) {
	// --- release.json ---
	raw, ok := files["release.json"]
	if !ok {
		b.add(Fail, "chk.missing", "release.json")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b.Manifest); err != nil {
		b.add(Fail, "chk.manifest_json", err.Error())
		return
	}
	m := &b.Manifest
	manifestOK := true
	fail := func(key string, args ...any) { b.add(Fail, key, args...); manifestOK = false }

	if m.SpecVersion != SpecVersion {
		fail("chk.spec_version", m.SpecVersion, SpecVersion)
	}
	if strings.TrimSpace(m.Repo) == "" {
		fail("chk.field_missing", "repo")
	} else if _, _, err := ParseRepo(m.Repo); err != nil {
		fail("chk.repo_invalid", m.Repo)
	} else if !SameRepo(m.Repo, expectRepo) {
		fail("chk.repo_mismatch", m.Repo, expectRepo)
	}
	if strings.TrimSpace(m.Version) == "" {
		fail("chk.field_missing", "version")
	} else if v, err := ParseVersion(m.Version); err != nil || strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Version)), "v") {
		fail("chk.version_format", m.Version)
	} else {
		b.Version = v
	}
	b.Tag = strings.TrimSpace(m.Tag)
	if b.Tag == "" && len(b.Version.Parts) > 0 {
		b.Tag = "v" + b.Version.String()
	}
	if b.Tag != "" && !tagRe.MatchString(b.Tag) {
		fail("chk.tag_format", b.Tag)
	}
	b.Title = strings.TrimSpace(m.Title)
	if b.Title == "" {
		b.Title = b.Tag
	}
	b.CommitMsg = strings.TrimSpace(m.CommitMessage)
	if b.CommitMsg == "" {
		b.CommitMsg = "Release " + b.Tag
	}
	if len(m.Languages) == 0 {
		fail("chk.field_missing", "languages")
	} else {
		seen := map[string]bool{}
		for _, l := range m.Languages {
			if !langRe.MatchString(l) || seen[l] {
				fail("chk.language_invalid", l)
				continue
			}
			seen[l] = true
		}
	}
	if manifestOK {
		b.add(Pass, "chk.manifest_ok", b.Tag, strings.Join(m.Languages, ", "))
		b.Languages = m.Languages
	}

	// --- required documents per language ---
	langs := m.Languages
	if len(langs) == 0 {
		langs = []string{"en"}
	}
	docsOK := true
	var notes []string
	for i := range langs {
		for _, base := range []string{"README", "RELEASE_NOTES"} {
			n := docName(base, langs, i)
			d, ok := files[n]
			if !ok {
				b.add(Fail, "chk.missing", n)
				docsOK = false
				continue
			}
			if len(bytes.TrimSpace(d)) == 0 {
				b.add(Fail, "chk.empty_doc", n)
				docsOK = false
				continue
			}
			if base == "RELEASE_NOTES" {
				notes = append(notes, strings.TrimSpace(string(bytes.TrimPrefix(d, []byte("\xef\xbb\xbf")))))
			}
		}
	}
	if docsOK {
		b.add(Pass, "chk.docs_ok", len(langs))
		b.ReleaseBody = strings.Join(notes, "\n\n---\n\n")
		b.ReadmeMain = files["README.md"]
	}

	// --- src/ snapshot ---
	var repoFiles []RepoFile
	ignored := []string{}
	for name, data := range files {
		switch {
		case strings.HasPrefix(name, "src/"):
			rel := strings.TrimPrefix(name, "src/")
			if isRootDoc(rel, langs) {
				b.add(Fail, "chk.doc_in_src", rel)
				continue
			}
			if rel == ".git" || strings.HasPrefix(rel, ".git/") {
				b.add(Fail, "chk.git_dir")
				continue
			}
			repoFiles = append(repoFiles, RepoFile{Path: rel, Data: data})
		case strings.HasPrefix(name, "dist/"), name == "release.json", isRootDoc(name, langs):
		default:
			ignored = append(ignored, name)
		}
	}
	if len(repoFiles) == 0 {
		b.add(Fail, "chk.src_empty")
	} else {
		b.add(Pass, "chk.src_ok", len(repoFiles))
	}
	if len(ignored) > 0 {
		sort.Strings(ignored)
		b.add(Warn, "chk.ignored", strings.Join(limitList(ignored, 5), ", "))
	}
	for i := range langs {
		for _, base := range []string{"README", "RELEASE_NOTES"} {
			n := docName(base, langs, i)
			if d, ok := files[n]; ok {
				repoFiles = append(repoFiles, RepoFile{Path: n, Data: d})
			}
		}
	}
	// Case-insensitive collisions break checkouts on Windows and macOS.
	lower := map[string]string{}
	for _, f := range repoFiles {
		k := strings.ToLower(f.Path)
		if o, ok := lower[k]; ok && o != f.Path {
			b.add(Fail, "chk.case_collision", o, f.Path)
		}
		lower[k] = f.Path
	}
	big := false
	for i := range repoFiles {
		f := &repoFiles[i]
		f.SHA = GitBlobSHA(f.Data)
		if len(f.Data) > maxSourceFile {
			b.add(Fail, "chk.file_too_big", f.Path, mb(len(f.Data)))
			big = true
		} else if len(f.Data) > warnSourceFile {
			b.add(Warn, "chk.file_big", f.Path, mb(len(f.Data)))
			big = true
		}
	}
	if !big {
		b.add(Pass, "chk.sizes_ok")
	}
	sort.Slice(repoFiles, func(i, j int) bool { return repoFiles[i].Path < repoFiles[j].Path })
	b.Files = repoFiles

	// --- assets ---
	seenAsset := map[string]bool{}
	assetsOK := true
	for _, a := range m.Assets {
		a = strings.ReplaceAll(strings.TrimSpace(a), "\\", "/")
		if !strings.HasPrefix(a, "dist/") {
			b.add(Fail, "chk.asset_not_dist", a)
			assetsOK = false
			continue
		}
		d, ok := files[a]
		if !ok {
			b.add(Fail, "chk.asset_missing", a)
			assetsOK = false
			continue
		}
		name := path.Base(a)
		if seenAsset[strings.ToLower(name)] {
			b.add(Fail, "chk.asset_dup", name)
			assetsOK = false
			continue
		}
		seenAsset[strings.ToLower(name)] = true
		if len(d) == 0 {
			b.add(Fail, "chk.asset_empty", a)
			assetsOK = false
			continue
		}
		if int64(len(d)) >= maxAssetFile {
			b.add(Fail, "chk.asset_too_big", a)
			assetsOK = false
			continue
		}
		b.Assets = append(b.Assets, Asset{Name: name, Data: d})
	}
	if assetsOK {
		b.add(Pass, "chk.assets_ok", len(b.Assets))
	}

	// --- secrets ---
	scanSecrets(b, files)
}

func mb(n int) string { return fmt.Sprintf("%.1f MB", float64(n)/1e6) }

var secretPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"GitHub token", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}\b`)},
	{"GitHub fine-grained token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9]{22}_[A-Za-z0-9]{59}\b`)},
	{"AWS access key", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	{"Private key", regexp.MustCompile(`-----BEGIN ([A-Z]+ )?PRIVATE KEY-----`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9]{32,}\b`)},
}

var secretFileNames = []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", ".npmrc", ".pypirc", ".netrc", ".git-credentials"}
var secretExts = []string{".pem", ".key", ".pfx", ".p12", ".keystore", ".jks"}

func isSecretFile(name string) bool {
	base := strings.ToLower(path.Base(name))
	if base == ".env" || (strings.HasPrefix(base, ".env.") && !strings.HasSuffix(base, ".example") && !strings.HasSuffix(base, ".sample") && !strings.HasSuffix(base, ".template")) {
		return true
	}
	for _, n := range secretFileNames {
		if base == n {
			return true
		}
	}
	for _, e := range secretExts {
		if strings.HasSuffix(base, e) {
			return true
		}
	}
	return false
}

func scanSecrets(b *Bundle, files map[string][]byte) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	found := false
	for _, n := range names {
		if isSecretFile(n) {
			b.add(Fail, "chk.secret_file", n)
			found = true
			continue
		}
		data := files[n]
		head := data
		if len(head) > 8000 {
			head = head[:8000]
		}
		binary := bytes.IndexByte(head, 0) >= 0
		for _, p := range secretPatterns {
			if p.re.Match(data) {
				// Never show the matched text itself.
				if binary {
					// Compiled programs pack strings together, which can look like a token by chance.
					b.add(Warn, "chk.secret_binary", n, p.kind)
				} else {
					b.add(Fail, "chk.secret_found", n, p.kind)
				}
				found = true
				break
			}
		}
	}
	if !found {
		b.add(Pass, "chk.secrets_ok")
	}
}

// IsWorkflowPath reports whether a repository path is a GitHub Actions workflow.
func IsWorkflowPath(p string) bool {
	return strings.HasPrefix(p, ".github/workflows/")
}

// PeekRepo reads only release.json from a bundle and returns its repo field ("" if unknown).
func PeekRepo(zipPath string) string {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return ""
	}
	defer zr.Close()
	var cand *zip.File
	for _, f := range zr.File {
		n, ok := cleanZipName(f.Name)
		if !ok {
			continue
		}
		if n == "release.json" {
			cand = f
			break
		}
		if cand == nil && strings.Count(n, "/") == 1 && strings.HasSuffix(n, "/release.json") {
			cand = f
		}
	}
	if cand == nil || cand.UncompressedSize64 > 1<<20 {
		return ""
	}
	rc, err := cand.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	var m struct {
		Repo string `json:"repo"`
	}
	data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil || json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &m) != nil {
		return ""
	}
	return strings.TrimSpace(m.Repo)
}
