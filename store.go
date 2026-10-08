package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Protector encrypts tokens. On Windows this is DPAPI (current user).
type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}

const dataFormat = 1

// Token states stored after the last check.
const (
	TokOK      = "ok"
	TokNoWrite = "nowrite"
	TokNoAcc   = "noaccess"
	TokUnknown = "unknown"
)

// History statuses.
const (
	StSuccess   = "success"   // committed and published
	StDraft     = "draft"     // committed, release saved as draft
	StFailed    = "failed"    // nothing changed on GitHub
	StCommitted = "committed" // commit pushed, release missing or incomplete
	StDeleted   = "deleted"   // release no longer exists on GitHub
	StExternal  = "external"  // release found on GitHub, not made by this program
	StRemoved   = "removed"   // release and tag deleted with this program (commit kept)
	StReverted  = "reverted"  // files restored to an earlier release by a new commit
)

type HistoryEntry struct {
	Time       string   `json:"time"`
	Version    string   `json:"version"`
	Tag        string   `json:"tag"`
	Title      string   `json:"title,omitempty"`
	Status     string   `json:"status"`
	Branch     string   `json:"branch,omitempty"`
	Added      int      `json:"added"`
	Modified   int      `json:"modified"`
	Deleted    int      `json:"deleted"`
	CommitSHA  string   `json:"commit_sha,omitempty"`
	CommitURL  string   `json:"commit_url,omitempty"`
	ReleaseID  int64    `json:"release_id,omitempty"`
	ReleaseURL string   `json:"release_url,omitempty"`
	Draft      bool     `json:"draft,omitempty"`
	ErrKey     string   `json:"error_key,omitempty"`
	ErrArgs    []string `json:"error_args,omitempty"`
	BundlePath string   `json:"bundle_path,omitempty"`
}

type RepoEntry struct {
	Owner        string          `json:"owner"`
	Name         string          `json:"name"`
	Branch       string          `json:"branch,omitempty"`       // empty = repository default
	RepoID       int64           `json:"repo_id,omitempty"`      // GitHub's permanent repository id
	FormerNames  []string        `json:"former_names,omitempty"` // previous owner/name, newest last
	TokenEnc     string          `json:"token_enc"`
	TokenHint    string          `json:"token_hint"`
	TokenExpires string          `json:"token_expires,omitempty"` // RFC3339
	TokenState   string          `json:"token_state"`
	LastCheck    string          `json:"last_check,omitempty"`
	History      []*HistoryEntry `json:"history"`
}

func (r *RepoEntry) Full() string { return r.Owner + "/" + r.Name }
func (r *RepoEntry) URL() string  { return "https://github.com/" + r.Full() }

type Data struct {
	Format   int          `json:"format"`
	App      string       `json:"app"`
	Language string       `json:"language"`
	Repos    []*RepoEntry `json:"repos"`
	// LastRepo is the repository last selected on the release page.
	LastRepo string `json:"last_repo,omitempty"`
	// SkipUpdate is a newer version the user chose not to be told about again.
	SkipUpdate string `json:"skip_update,omitempty"`
	// Archived keeps history of removed repositories, keyed by lower-case owner/name.
	Archived map[string][]*HistoryEntry `json:"archived,omitempty"`
}

// Store is the single portable data file next to the executable.
type Store struct {
	mu   sync.Mutex
	Path string
	D    Data
	P    Protector
}

var ErrNeedToken = errors.New("token must be entered again on this PC")

func LoadStore(path string, p Protector) (*Store, error) {
	s := &Store{Path: path, P: p, D: Data{Format: dataFormat, App: "GitHub Relay"}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.D); err != nil {
		return nil, err
	}
	for _, r := range s.D.Repos {
		if r.History == nil {
			r.History = []*HistoryEntry{}
		}
	}
	return s, nil
}

// Save writes atomically: temp file in the same folder, then rename.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.D.Format = dataFormat
	s.D.App = "GitHub Relay"
	b, err := json.MarshalIndent(&s.D, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.Path)
	tmp, err := os.CreateTemp(dir, ".GithubRelay-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func TokenHint(tok string) string {
	tok = strings.TrimSpace(tok)
	prefix := ""
	for _, p := range []string{"github_pat_", "ghp_"} {
		if strings.HasPrefix(tok, p) {
			prefix = p
		}
	}
	if len(tok) < len(prefix)+8 {
		return prefix + "…"
	}
	return prefix + "…" + tok[len(tok)-4:]
}

func (s *Store) SetToken(r *RepoEntry, tok string) error {
	enc, err := s.P.Protect([]byte(tok))
	if err != nil {
		return err
	}
	r.TokenEnc = base64.StdEncoding.EncodeToString(enc)
	r.TokenHint = TokenHint(tok)
	return nil
}

func (s *Store) Token(r *RepoEntry) (string, error) {
	if r.TokenEnc == "" {
		return "", ErrNeedToken
	}
	raw, err := base64.StdEncoding.DecodeString(r.TokenEnc)
	if err != nil {
		return "", ErrNeedToken
	}
	b, err := s.P.Unprotect(raw)
	if err != nil {
		return "", ErrNeedToken
	}
	return string(b), nil
}

func (s *Store) Find(full string) *RepoEntry {
	for _, r := range s.D.Repos {
		if SameRepo(r.Full(), full) {
			return r
		}
	}
	return nil
}

func (s *Store) Remove(r *RepoEntry) {
	out := s.D.Repos[:0]
	for _, x := range s.D.Repos {
		if x != r {
			out = append(out, x)
		}
	}
	s.D.Repos = out
}

// TokenStatus is what the repository list shows.
type TokenStatus int

const (
	TSOk TokenStatus = iota
	TSExpiring
	TSExpired
	TSNoAccess
	TSNoWrite
	TSNeedToken
	TSUnknown
)

func (s *Store) Status(r *RepoEntry, now time.Time) (TokenStatus, time.Time) {
	if _, err := s.Token(r); err != nil {
		return TSNeedToken, time.Time{}
	}
	var exp time.Time
	if r.TokenExpires != "" {
		exp, _ = time.Parse(time.RFC3339, r.TokenExpires)
	}
	if !exp.IsZero() && now.After(exp) {
		return TSExpired, exp
	}
	switch r.TokenState {
	case TokNoAcc:
		return TSNoAccess, exp
	case TokNoWrite:
		return TSNoWrite, exp
	case TokUnknown:
		return TSUnknown, exp
	}
	if !exp.IsZero() && exp.Sub(now) < 14*24*time.Hour {
		return TSExpiring, exp
	}
	return TSOk, exp
}

// LatestSuccess returns the newest history entry that reached GitHub.
func (r *RepoEntry) LatestSuccess() *HistoryEntry {
	var best *HistoryEntry
	var bestV Version
	for _, h := range r.History {
		if h.Status != StSuccess && h.Status != StDraft && h.Status != StExternal && h.Status != StCommitted {
			continue
		}
		v, err := ParseVersion(h.Version)
		if err != nil {
			continue
		}
		if best == nil || v.Compare(bestV) > 0 {
			best, bestV = h, v
		}
	}
	return best
}

func (h *HistoryEntry) SetError(key string, args ...any) {
	h.ErrKey = key
	h.ErrArgs = nil
	for _, a := range args {
		h.ErrArgs = append(h.ErrArgs, fmt.Sprint(a))
	}
}

func (h *HistoryEntry) ErrorText(lang string) string {
	if h.ErrKey == "" {
		return ""
	}
	a := make([]any, len(h.ErrArgs))
	for i, x := range h.ErrArgs {
		a[i] = x
	}
	return T(lang, h.ErrKey, a...)
}

// Archive keeps a removed repository's history so it returns on re-registration.
func (s *Store) Archive(r *RepoEntry) {
	if len(r.History) == 0 {
		return
	}
	if s.D.Archived == nil {
		s.D.Archived = map[string][]*HistoryEntry{}
	}
	s.D.Archived[strings.ToLower(r.Full())] = r.History
}

// TakeArchived returns and forgets archived history for a repository.
func (s *Store) TakeArchived(full string) []*HistoryEntry {
	k := strings.ToLower(full)
	h := s.D.Archived[k]
	delete(s.D.Archived, k)
	return h
}

// Names returns the current name followed by former names.
func (r *RepoEntry) Names() []string {
	return append([]string{r.Full()}, r.FormerNames...)
}

// KnownAs reports whether full is the current or a former name of this repository.
func (r *RepoEntry) KnownAs(full string) bool {
	for _, n := range r.Names() {
		if SameRepo(n, full) {
			return true
		}
	}
	return false
}

// Rename moves the registration to a new owner/name, keeping token and history.
func (s *Store) Rename(r *RepoEntry, newFull string) error {
	owner, name, err := ParseRepo(newFull)
	if err != nil {
		return err
	}
	old := r.Full()
	if SameRepo(old, newFull) {
		r.Owner, r.Name = owner, name // case change only
		return nil
	}
	keep := []string{}
	for _, n := range r.FormerNames {
		if !SameRepo(n, newFull) && !SameRepo(n, old) {
			keep = append(keep, n)
		}
	}
	r.FormerNames = append(keep, old)
	r.Owner, r.Name = owner, name
	if SameRepo(s.D.LastRepo, old) {
		s.D.LastRepo = r.Full()
	}
	return nil
}
