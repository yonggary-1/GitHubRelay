//go:build windows

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// dropPaths returns every file dropped onto the window.
func dropPaths(hdrop uintptr) []string {
	n, _, _ := pDragQueryFileW.Call(hdrop, 0xFFFFFFFF, 0, 0)
	var out []string
	buf := make([]uint16, 32768)
	for i := uintptr(0); i < n; i++ {
		pDragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		out = append(out, syscall.UTF16ToString(buf))
	}
	return out
}

func (a *App) batchFind(repo string) *RepoEntry {
	for _, r := range a.store.D.Repos {
		if r.KnownAs(repo) {
			return r
		}
	}
	return nil
}

// loadBatch runs the local pre-flight for several bundles.
func (a *App) loadBatch(paths []string) {
	if len(a.store.D.Repos) == 0 {
		a.warn(a.t("drop.need_repo"))
		return
	}
	a.bundlePath = ""
	a.clearPlan()
	a.batch = &Batch{}
	for _, p := range paths {
		a.batch.Items = append(a.batch.Items, &BatchItem{Path: p, Name: filepath.Base(p), State: BWait})
	}
	a.batchPaths = paths
	a.setBusy(true)
	a.setMarquee(true)
	a.setStatus("batch.preflight", len(paths))
	a.renderPlan()
	a.updateDropState()
	go func() {
		b := PreflightBatch(paths, a.batchFindSnapshot())
		a.post(func() {
			a.setMarquee(false)
			a.setBusy(false)
			if a.batch == nil { // reset meanwhile
				return
			}
			a.batch = b
			if b.Repo != nil {
				for i, r := range a.store.D.Repos {
					if r == b.Repo && i != a.relIdx {
						a.autoSel = true
						lvSelect(a.lvRelRepos, i)
						a.autoSel = false
					}
				}
			}
			if b.Failed() {
				a.setStatus("batch.blocked")
			} else {
				a.setStatus("batch.ready")
			}
			a.renderPlan()
		})
	}()
}

// batchFindSnapshot resolves repositories without touching live data from the worker.
func (a *App) batchFindSnapshot() func(string) *RepoEntry {
	repos := append([]*RepoEntry(nil), a.store.D.Repos...)
	return func(repo string) *RepoEntry {
		for _, r := range repos {
			if r.KnownAs(repo) {
				return r
			}
		}
		return nil
	}
}

func (a *App) batchStateText(s string) string { return a.t("bst." + s) }

func (a *App) itemNote(it *BatchItem) string {
	if it.NoteKey != "" {
		return a.t(it.NoteKey, it.NoteArgs...)
	}
	for _, c := range it.Checks {
		if c.Level == Fail {
			return "✖ " + c.Text(a.lang)
		}
	}
	if w := it.Warnings(); len(w) > 0 {
		return "⚠ " + w[0].Text(a.lang)
	}
	return ""
}

// renderBatch fills the batch list, summary and buttons.
func (a *App) renderBatch() {
	b := a.batch
	lvClear(a.lvBatch)
	for i, it := range b.Items {
		v := it.Version.String()
		if v == "" {
			v = a.t("none")
		}
		lvAdd(a.lvBatch, fmt.Sprint(i+1), it.Name, v, a.batchStateText(it.State), a.itemNote(it))
	}
	lvClear(a.lvChanges)
	if a.plan != nil {
		for _, c := range a.plan.Changes {
			k := map[ChangeKind]string{ChAdded: "ch.added", ChModified: "ch.modified", ChDeleted: "ch.deleted"}[c.Kind]
			lvAdd(a.lvChanges, a.t(k), c.Path)
		}
	}
	repo, first, last := a.t("none"), a.t("none"), a.t("none")
	if b.Repo != nil {
		repo = b.Repo.Full()
	}
	if n := len(b.Items); n > 0 {
		if v := b.Items[0].Version.String(); v != "" {
			first = v
		}
		if v := b.Items[n-1].Version.String(); v != "" {
			last = v
		}
	}
	s := a.t("batch.summary", len(b.Items), repo, first, last, b.Done(), len(b.Items))
	if b.NameOrderDiffers {
		s += "\r\n" + a.t("batch.name_order")
	}
	for _, c := range b.Checks {
		s += "\r\n✖ " + c.Text(a.lang)
	}
	setText(a.summary, s)
	a.updateUpload()
}

// updateBatchButtons is the batch part of updateUpload.
func (a *App) updateBatchButtons() {
	b := a.batch
	enable(a.ckDraft, false)
	setChecked(a.ckDraft, false)
	enable(a.ckApproveWF, false)
	enable(a.ckApproveWarn, false)
	switch {
	case a.batchRunning:
		setText(a.btnUpload, a.t("batch.stop"))
		enable(a.btnUpload, !a.batchStop)
	case b.Next() > 0:
		setText(a.btnUpload, a.t("batch.resume"))
		enable(a.btnUpload, !a.busy && !b.Failed())
	default:
		setText(a.btnUpload, a.t("batch.start"))
		enable(a.btnUpload, !a.busy && !b.Failed() && b.Repo != nil && b.Next() >= 0)
	}
	enable(a.btnOpenRel, a.lastRelURL != "")
}

func (a *App) onBatchButton() {
	b := a.batch
	if a.batchRunning {
		a.batchStop = true
		a.setStatus("batch.stopping")
		a.updateUpload()
		return
	}
	if a.busy || b.Failed() || b.Repo == nil {
		return
	}
	start := b.Next()
	if start < 0 {
		return
	}
	if a.relRepo() != b.Repo {
		a.warn(a.t("batch.repo_changed"))
		return
	}
	var warns []string
	for _, it := range b.Items[start:] {
		for _, c := range it.Warnings() {
			warns = append(warns, fmt.Sprintf("• %s: %s", it.Name, c.Text(a.lang)))
		}
	}
	wtext := ""
	if len(warns) > 0 {
		wtext = a.t("batch.confirm_warns", strings.Join(limitList(warns, 10), "\n"))
	}
	n := len(b.Items) - start
	if !a.ask(a.t("batch.confirm", b.Repo.Full(), n, b.Items[start].Version.String(), b.Items[len(b.Items)-1].Version.String(), wtext)) {
		return
	}
	token, err := a.store.Token(b.Repo)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	a.batchRunning, a.batchStop = true, false
	a.uploading = true
	a.setBusy(true)
	a.runBatchStep(token)
}

// stopBatch ends a run; key/args describe why.
func (a *App) stopBatch(key string, args ...any) {
	a.batchRunning, a.batchStop = false, false
	a.uploading = false
	a.setBusy(false)
	a.setStatus(key, args...)
	a.refreshRepos()
	a.refreshRelRepos()
	a.refreshHistory()
	a.refreshSpec()
	a.renderPlan()
}

func (a *App) runBatchStep(token string) {
	b := a.batch
	i := b.Next()
	if i < 0 {
		send(a.progress, PBM_SETPOS, 1000, 0)
		skipped := 0
		for _, it := range b.Items {
			if it.State == BSkipped {
				skipped++
			}
		}
		a.stopBatch("batch.done", b.Done(), skipped)
		return
	}
	if a.batchStop {
		a.stopBatch("batch.stopped", b.Done(), len(b.Items))
		return
	}
	it := b.Items[i]
	r := b.Repo
	total := len(b.Items)
	it.State, it.NoteKey = BRunning, ""
	a.plan = nil
	a.setStatus("batch.checking", i+1, total, it.Version.String())
	a.renderBatch()
	cp := &RepoEntry{Owner: r.Owner, Name: r.Name, Branch: r.Branch, History: cloneHistory(r.History), FormerNames: append([]string(nil), r.FormerNames...)}
	gh := NewGitHub(token)
	go func() {
		bd := LoadBundle(it.Path, cp.Full(), cp.FormerNames...)
		var p *Plan
		if bd.Version.Compare(it.Version) != 0 || len(bd.Version.Parts) == 0 {
			p = &Plan{Bundle: bd, Owner: cp.Owner, Name: cp.Name}
			p.add(Fail, "batch.changed")
		} else {
			p = Analyze(context.Background(), gh, cp, bd)
		}
		a.post(func() { a.batchAnalyzed(token, i, p) })
	}()
}

func (a *App) batchAnalyzed(token string, i int, p *Plan) {
	b := a.batch
	it := b.Items[i]
	total := len(b.Items)
	a.plan = p
	if AlreadyReleased(p) {
		it.State = BSkipped
		it.SetNote("batch.note_skipped")
		a.renderBatch()
		a.runBatchStep(token)
		return
	}
	if !p.CanUpload() {
		var lines []string
		for _, c := range p.AllChecks() {
			if c.Level == Fail {
				lines = append(lines, "✖ "+c.Text(a.lang))
			}
		}
		it.State = BFailed
		it.SetNote("batch.note_fail", strings.Join(lines, " / "))
		a.renderBatch()
		a.stopBatch("batch.failed_at", i+1, total, it.Version.String(), strings.Join(lines, " / "))
		a.warn(a.t("batch.failed_at", i+1, total, it.Version.String(), strings.Join(lines, "\n")))
		return
	}
	// Warnings that need GitHub to detect are confirmed here, one bundle at a time.
	var warns []string
	for _, c := range p.Checks {
		if c.Level == Warn {
			warns = append(warns, "⚠ "+c.Text(a.lang))
		}
	}
	a.renderBatch()
	if len(warns) > 0 && !a.ask(a.t("batch.warn_q", i+1, total, it.Version.String(), strings.Join(warns, "\n"))) {
		it.State = BPaused
		it.SetNote("batch.note_paused")
		a.stopBatch("batch.paused", it.Version.String())
		return
	}
	if a.batchStop {
		it.State = BWait
		a.stopBatch("batch.stopped", b.Done(), total)
		return
	}
	r := b.Repo
	go func() {
		h := Upload(context.Background(), NewGitHub(token), p, false, func(done, n int, key string, args ...any) {
			a.post(func() {
				if n > 0 {
					send(a.progress, PBM_SETPOS, uintptr((i*1000+done*1000/n)/total), 0)
				}
				a.setStatus("batch.step", i+1, total, it.Version.String(), a.t(key, args...))
			})
		})
		a.post(func() {
			r.History = append([]*HistoryEntry{h}, r.History...)
			a.save()
			switch h.Status {
			case StSuccess, StDraft:
				it.State = BDone
				it.SetNote("batch.note_done", h.ReleaseURL)
				a.lastRelURL = h.ReleaseURL
				a.refreshRelRepos()
				a.runBatchStep(token)
			case StCommitted:
				it.State = BFailed
				it.SetNote("batch.note_fail", h.ErrorText(a.lang))
				a.stopBatch("st.committed", h.ErrorText(a.lang))
			default:
				it.State = BFailed
				it.SetNote("batch.note_fail", h.ErrorText(a.lang))
				a.stopBatch("batch.failed_at", i+1, total, it.Version.String(), h.ErrorText(a.lang))
			}
		})
	}()
}

// showBatchItem opens the full check list of one bundle.
func (a *App) showBatchItem(i int) {
	if a.batch == nil || i < 0 || i >= len(a.batch.Items) {
		return
	}
	it := a.batch.Items[i]
	body := a.checksText(it.Checks)
	if body == "" {
		body = a.t("batch.no_issues")
	}
	a.info(a.t("batch.detail", it.Name, it.Version.String(), strings.ReplaceAll(body, "\r\n", "\n")))
}
