//go:build windows

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	mbDefButton2 = 0x100
	mbDefButton3 = 0x200
)

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
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	cp := &RepoEntry{Owner: r.Owner, Name: r.Name, Branch: r.Branch}
	target := *h
	gh := NewGitHub(token)
	a.setBusy(true)
	a.setHist(func() string { return a.t("del.checking", target.Version) })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		dp, err := PlanDelete(ctx, gh, cp, &target)
		a.post(func() {
			if err != nil {
				a.setBusy(false)
				k, args := explainAPIError(err)
				a.setHist(func() string { return a.t("st.failed", a.t(k, args...)) })
				return
			}
			revert := false
			if dp.Revert != nil {
				pv := dp.Revert.Entry.Version
				q := a.t("del.confirm_latest", r.Full(), target.Version, target.Tag, pv, dp.Revert.Added, dp.Revert.Modified, dp.Revert.Deleted)
				switch msgBox(a.hwnd, q, "GitHub Relay", MB_YESNOCANCEL|MB_ICONWARNING|mbDefButton3) {
				case IDYES:
					revert = true
				case IDNO:
				default:
					a.setBusy(false)
					a.setHist(func() string { return "" })
					return
				}
			} else if !a.askCareful(a.t("del.confirm", r.Full(), target.Version, target.Tag)) {
				a.setBusy(false)
				a.setHist(func() string { return "" })
				return
			}
			a.uploading = true
			a.setHist(func() string { return a.t("del.running", target.Version) })
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				var rh *HistoryEntry
				if revert {
					rh = ExecuteRevert(ctx, gh, cp, dp.Revert)
				}
				var derr error
				if rh == nil || rh.Status == StReverted {
					derr = DeleteReleaseEntry(ctx, gh, cp, &target)
				}
				a.post(func() {
					a.uploading = false
					a.setBusy(false)
					if rh != nil {
						r.History = append([]*HistoryEntry{rh}, r.History...)
					}
					switch {
					case rh != nil && rh.Status != StReverted:
						a.setHist(func() string { return a.t("del.revert_failed", rh.ErrorText(a.lang)) })
					case derr != nil:
						k, args := explainAPIError(derr)
						a.setHist(func() string { return a.t("st.failed", a.t(k, args...)) })
					default:
						*h = target
						if rh != nil {
							pv := rh.Version
							a.setHist(func() string { return a.t("del.done_reverted", target.Version, pv) })
						} else {
							a.setHist(func() string { return a.t("del.done", target.Version) })
						}
					}
					a.save()
					a.refreshRepos()
					a.refreshHistory()
					a.refreshSpec()
				})
			}()
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
	gh := NewGitHub(token)
	a.setBusy(true)
	a.setHist(func() string { return a.t("rev.checking", target.Version) })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		rp, err := PlanRevert(ctx, gh, cp, &target)
		a.post(func() {
			if err != nil {
				a.setBusy(false)
				k, args := explainAPIError(err)
				a.setHist(func() string { return a.t("st.failed", a.t(k, args...)) })
				return
			}
			var later []string
			for _, rel := range rp.Later {
				later = append(later, rel.TagName)
			}
			if rp.NoChange() && len(rp.Later) == 0 {
				a.setBusy(false)
				a.setHist(func() string { return a.t("rev.same", target.Version) })
				a.info(a.t("rev.same", target.Version))
				return
			}
			doRevert := !rp.NoChange()
			if doRevert {
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
			}
			deleteLater := false
			if len(later) > 0 {
				key := "rev.later_q"
				if !doRevert {
					key = "rev.later_only_q"
				}
				deleteLater = a.askCareful(a.t(key, target.Version, len(later), strings.Join(limitList(later, 20), ", ")))
				if !doRevert && !deleteLater {
					a.setBusy(false)
					a.setHist(func() string { return "" })
					return
				}
			}
			a.uploading = true
			a.setHist(func() string { return a.t("rev.running", target.Version) })
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				var res *HistoryEntry
				if doRevert {
					res = ExecuteRevert(ctx, gh, cp, rp)
				}
				var deleted []Release
				var derr error
				if deleteLater && (res == nil || res.Status == StReverted) {
					deleted, derr = DeleteLaterReleases(ctx, gh, cp, rp.Later)
				}
				a.post(func() {
					a.uploading = false
					a.setBusy(false)
					if res != nil {
						r.History = append([]*HistoryEntry{res}, r.History...)
					}
					for _, rel := range deleted {
						MarkRemoved(r, rel)
					}
					a.save()
					a.refreshRepos()
					a.refreshHistory()
					a.refreshSpec()
					switch {
					case res != nil && res.Status != StReverted:
						a.setHist(func() string { return a.t("st.failed", res.ErrorText(a.lang)) })
					case derr != nil:
						k, args := explainAPIError(derr)
						n := len(deleted)
						a.setHist(func() string { return a.t("rev.later_failed", n, len(later), a.t(k, args...)) })
					case deleteLater:
						n := len(deleted)
						a.setHist(func() string { return a.t("rev.done_later", target.Version, n) })
					default:
						a.setHist(func() string { return a.t("rev.done", target.Version) })
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
		gh := NewGitHub("")
		if updateAPIBase != "" {
			gh.APIBase = updateAPIBase
		}
		u, err := CheckUpdate(ctx, gh)
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
				key := "upd.found"
				if u.AssetURL == "" {
					key = "upd.found_page"
				}
				switch msgBox(a.hwnd, a.t(key, v, AppVersion), "GitHub Relay", MB_YESNOCANCEL|MB_ICONINFORMATION) {
				case IDYES:
					if u.AssetURL == "" {
						openURL(u.URL)
					} else {
						a.applyUpdate(u)
					}
				case IDNO:
				default:
					a.store.D.SkipUpdate = v
					a.save()
				}
			}
		})
	}()
}

// updateAPIBase is empty in release builds. Test builds may set it with
// -ldflags "-X main.updateAPIBase=http://127.0.0.1:port" to try the update locally.
var updateAPIBase string

// updatedFlag is passed to the new program after an update.
const updatedFlag = "--updated"

// instanceMutex keeps one window per data file; it is released before an update restarts the program.
var instanceMutex uintptr

// applyUpdate downloads, verifies and installs the new version, then restarts.
func (a *App) applyUpdate(u *UpdateInfo) {
	if a.busy || a.uploading || a.batchRunning {
		a.warn(a.t("upd.busy"))
		return
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		a.warn(a.t("upd.failed", err.Error()))
		return
	}
	tmp := exe + ".new"
	a.setBusy(true)
	enable(a.btnUpdate, false)
	a.setStatus("upd.downloading", u.Version.String())
	a.setMarquee(true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := DownloadUpdate(ctx, &http.Client{Timeout: 5 * time.Minute}, u, tmp)
		a.post(func() {
			a.setMarquee(false)
			fail := func(e error) {
				os.Remove(tmp)
				a.setBusy(false)
				enable(a.btnUpdate, true)
				a.setStatus("")
				msg := e.Error()
				if e == errUpdSize || e == errUpdDigest || e == errUpdFormat {
					msg = a.t(e.Error())
				} else if k, args := explainAPIError(e); k != "err.network" {
					msg = a.t(k, args...)
				}
				if a.ask(a.t("upd.failed_page", msg)) {
					openURL(u.URL)
				}
			}
			if err != nil {
				fail(err)
				return
			}
			if !a.save() {
				os.Remove(tmp)
				a.setBusy(false)
				enable(a.btnUpdate, true)
				return
			}
			if err := SwapProgram(exe, tmp); err != nil {
				fail(err)
				return
			}
			// Hand over: release the single-instance lock, start the new version, close this one.
			if instanceMutex != 0 {
				pCloseHandle.Call(instanceMutex)
				instanceMutex = 0
			}
			cmd := exec.Command(exe, updatedFlag)
			cmd.Dir = filepath.Dir(exe)
			if err := cmd.Start(); err != nil {
				a.warn(a.t("upd.restart_failed", err.Error()))
			}
			pDestroyWindow.Call(a.hwnd)
		})
	}()
}
