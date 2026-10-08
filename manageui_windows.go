//go:build windows

package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const mbDefButton2 = 0x100

// askCareful is a yes/no question whose default answer is No.
func (a *App) askCareful(s string) bool {
	return msgBox(a.hwnd, s, "GitHub Relay", MB_YESNO|MB_ICONWARNING|mbDefButton2) == IDYES
}

// ---------- delete a release ----------

func (a *App) onDeleteRelease() {
	r := comboRepo(a, a.cbHistRepo, 0)
	h := a.selectedHist()
	if r == nil || h == nil {
		a.warn(a.t("msg.select_entry"))
		return
	}
	if !CanDeleteRelease(h) || a.busy {
		return
	}
	if !a.askCareful(a.t("del.confirm", r.Full(), h.Version, h.Tag)) {
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	cp := *h
	a.setBusy(true)
	a.setHist(func() string { return a.t("del.running", cp.Version) })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := DeleteReleaseEntry(ctx, NewGitHub(token), r, &cp)
		a.post(func() {
			a.setBusy(false)
			if err != nil {
				k, args := explainAPIError(err)
				a.refreshHistory()
				a.setHist(func() string { return a.t("st.failed", a.t(k, args...)) })
				return
			}
			*h = cp
			a.save()
			a.refreshRepos()
			a.refreshHistory()
			a.refreshSpec()
			a.setHist(func() string { return a.t("del.done", cp.Version) })
		})
	}()
}

// ---------- revert to an earlier version ----------

func (a *App) onRevert() {
	r := comboRepo(a, a.cbHistRepo, 0)
	h := a.selectedHist()
	if r == nil || h == nil {
		a.warn(a.t("msg.select_entry"))
		return
	}
	if !CanRevert(h) || a.busy {
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	cp := &RepoEntry{Owner: r.Owner, Name: r.Name, Branch: r.Branch}
	target := *h
	a.setBusy(true)
	a.setHist(func() string { return a.t("rev.checking", target.Version) })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		gh := NewGitHub(token)
		rp, err := PlanRevert(ctx, gh, cp, &target)
		a.post(func() {
			if err != nil {
				a.setBusy(false)
				k, args := explainAPIError(err)
				a.setHist(func() string { return a.t("st.failed", a.t(k, args...)) })
				return
			}
			if rp.NoChange() {
				a.setBusy(false)
				a.setHist(func() string { return a.t("rev.same", target.Version) })
				a.info(a.t("rev.same", target.Version))
				return
			}
			var lines []string
			for _, c := range rp.Changes {
				k := map[ChangeKind]string{ChAdded: "ch.added", ChModified: "ch.modified", ChDeleted: "ch.deleted"}[c.Kind]
				lines = append(lines, fmt.Sprintf("  %s  %s", a.t(k), c.Path))
			}
			q := a.t("rev.confirm", r.Full(), target.Version, rp.Branch, rp.Added, rp.Modified, rp.Deleted, strings.Join(limitList(lines, 15), "\n"))
			if !a.askCareful(q) {
				a.setBusy(false)
				a.setHist(func() string { return "" })
				return
			}
			a.uploading = true
			a.setHist(func() string { return a.t("rev.running", target.Version) })
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				res := ExecuteRevert(ctx, gh, cp, rp)
				a.post(func() {
					a.uploading = false
					a.setBusy(false)
					r.History = append([]*HistoryEntry{res}, r.History...)
					a.save()
					a.refreshRepos()
					a.refreshHistory()
					if res.Status == StReverted {
						a.setHist(func() string { return a.t("rev.done", target.Version) })
					} else {
						a.setHist(func() string { return a.t("st.failed", res.ErrorText(a.lang)) })
					}
				})
			}()
		})
	}()
}

// ---------- update check ----------

// checkUpdateAsync asks GitHub for a newer GitHub Relay. When manual is false (startup),
// errors and skipped versions stay silent.
func (a *App) checkUpdateAsync(manual bool) {
	if manual {
		enable(a.btnUpdate, false)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		u, err := CheckUpdate(ctx, NewGitHub(""))
		a.post(func() {
			if manual {
				enable(a.btnUpdate, true)
			}
			switch {
			case err != nil:
				if manual {
					k, args := explainAPIError(err)
					a.warn(a.t("upd.failed", a.t(k, args...)))
				}
			case u == nil:
				if manual {
					a.info(a.t("upd.latest", AppVersion))
				}
			default:
				v := u.Version.String()
				if !manual && a.store.D.SkipUpdate == v {
					return
				}
				switch msgBox(a.hwnd, a.t("upd.found", v, AppVersion), "GitHub Relay", MB_YESNOCANCEL|MB_ICONINFORMATION) {
				case IDYES:
					openURL(u.URL)
				case IDNO:
				default:
					a.store.D.SkipUpdate = v
					a.save()
				}
			}
		})
	}()
}
