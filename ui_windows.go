//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const wmRun = WM_APP + 1

// Page indexes.
const (
	pgRelease = iota
	pgHistory
	pgSpec
	pgRepos
	pgAll = -1
)

type control struct {
	h       uintptr
	page    int
	textKey string // "" = text set manually
	cueKey  string
}

type App struct {
	hwnd   uintptr
	hinst  uintptr
	dpi    int
	font   uintptr
	bold   uintptr
	lang   string
	store  *Store
	ctrls  []*control
	byID   map[uint32]func(code uint32)
	nextID uint32
	page   int

	qmu   sync.Mutex
	queue []func()

	busy      bool
	uploading bool

	// header / frame
	title, radKO, radEN, tab, disclaimer, btnAbout uintptr

	// repositories page
	lvRepos, lblURL, edURL, lblTok, edTok, lblBr, edBr                                    uintptr
	btnRegister, btnReplace, btnVerify, btnRemove, btnOpenRepo, btnTokenPage, btnEditAddr uintptr
	guide, edResult                                                                       uintptr

	// release page
	lblRelRepo, lvRelRepos, btnBrowse, btnReanalyze, btnReset, dropFrame, dropMsg, summary uintptr
	relIdx                                                                                 int  // selected repository on the release page
	autoSel                                                                                bool // selection changed by the program, not the user
	lvChecks, lvChanges, ckDraft, ckApproveWF, ckApproveWarn                               uintptr
	status, progress, btnOpenRel, btnUpload                                                uintptr
	bundlePath                                                                             string
	plan                                                                                   *Plan
	planUsed                                                                               bool
	lastRelURL                                                                             string
	statusKey                                                                              string
	statusArgs                                                                             []any

	// history page
	lblHistRepo, cbHistRepo, btnSync, btnHistOpen, btnHistCommit, btnRetry, lvHist, histStatus uintptr
	histRows                                                                                   []*HistoryEntry

	// spec page
	lblSpecRepo, cbSpecRepo, btnCopy, btnSaveMD, edSpec uintptr

	selRepo int // selection in the repositories list

	resultFn   func() string // result box text, rebuilt when the language changes
	histFn     func() string // history status text, rebuilt when the language changes
	specPicked bool          // the user chose the spec target themselves
}

var app *App

func (a *App) s(v int) int { return v * a.dpi / 96 }

func (a *App) t(key string, args ...any) string { return T(a.lang, key, args...) }

// post runs f on the UI thread.
func (a *App) post(f func()) {
	a.qmu.Lock()
	a.queue = append(a.queue, f)
	a.qmu.Unlock()
	pPostMessageW.Call(a.hwnd, wmRun, 0, 0)
}

func (a *App) drain() {
	a.qmu.Lock()
	q := a.queue
	a.queue = nil
	a.qmu.Unlock()
	for _, f := range q {
		f()
	}
}

// ---------- control creation ----------

func (a *App) create(class string, style uint32, exStyle uint32, page int, textKey string, onCmd func(code uint32)) uintptr {
	a.nextID++
	id := a.nextID
	text := ""
	if textKey != "" {
		text = a.t(textKey)
	}
	h, _, _ := pCreateWindowExW.Call(uintptr(exStyle), uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
		uintptr(style|WS_CHILD), 0, 0, 10, 10, a.hwnd, uintptr(id), a.hinst, 0)
	send(h, WM_SETFONT, a.font, 1)
	a.ctrls = append(a.ctrls, &control{h: h, page: page, textKey: textKey})
	if onCmd != nil {
		a.byID[id] = onCmd
	}
	return h
}

func (a *App) button(page int, key string, f func()) uintptr {
	return a.create("BUTTON", WS_TABSTOP|BS_PUSHBUTTON, 0, page, key, func(c uint32) {
		if c == BN_CLICKED {
			f()
		}
	})
}

func (a *App) check(page int, key string, f func()) uintptr {
	return a.create("BUTTON", WS_TABSTOP|BS_AUTOCHECKBOX|BS_MULTILINE, 0, page, key, func(c uint32) {
		if c == BN_CLICKED {
			f()
		}
	})
}

func (a *App) label(page int, key string) uintptr {
	return a.create("STATIC", SS_LEFT|SS_NOPREFIX|SS_CENTERIMAGE, 0, page, key, nil)
}

func (a *App) wrapLabel(page int, key string) uintptr {
	return a.create("STATIC", SS_LEFT|SS_NOPREFIX, 0, page, key, nil)
}

func (a *App) edit(page int, extra uint32, cue string) uintptr {
	h := a.create("EDIT", WS_TABSTOP|ES_AUTOHSCROLL|extra, WS_EX_CLIENTEDGE, page, "", nil)
	a.ctrls[len(a.ctrls)-1].cueKey = cue
	return h
}

func (a *App) combo(page int, f func()) uintptr {
	return a.create("COMBOBOX", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWNLIST|CBS_HASSTRINGS, 0, page, "", func(c uint32) {
		if c == CBN_SELCHANGE {
			f()
		}
	})
}

func (a *App) listView(page int, cols []string) uintptr {
	h := a.create("SysListView32", WS_TABSTOP|WS_BORDER|LVS_REPORT|LVS_SINGLESEL|LVS_SHOWSELALWAYS|LVS_NOSORTHEADER, 0, page, "", nil)
	send(h, LVM_SETEXTENDEDLISTVIEWSTYLE, 0, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER|LVS_EX_LABELTIP|LVS_EX_GRIDLINES)
	for i, k := range cols {
		col := LVCOLUMNW{Mask: LVCF_TEXT | LVCF_WIDTH | LVCF_SUBITEM, Cx: 100, PszText: u16(a.t(k)), ISubItem: int32(i)}
		send(h, LVM_INSERTCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	lvCols[h] = cols
	return h
}

var lvCols = map[uintptr][]string{}

func lvClear(h uintptr) { send(h, LVM_DELETEALLITEMS, 0, 0) }

func lvAdd(h uintptr, cells ...string) {
	n := int32(send(h, 0x1004 /*LVM_GETITEMCOUNT*/, 0, 0))
	it := LVITEMW{Mask: LVIF_TEXT, IItem: n, PszText: u16(cells[0])}
	idx := send(h, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&it)))
	for i := 1; i < len(cells); i++ {
		sub := LVITEMW{ISubItem: int32(i), PszText: u16(cells[i])}
		send(h, LVM_SETITEMTEXTW, idx, uintptr(unsafe.Pointer(&sub)))
	}
}

func lvSelected(h uintptr) int {
	r := send(h, LVM_GETNEXTITEM, ^uintptr(0), LVNI_SELECTED)
	return int(int32(r))
}

func lvSelect(h uintptr, i int) {
	st := LVITEMW{State: LVIS_SELECTED | LVIS_FOCUSED, StateMask: LVIS_SELECTED | LVIS_FOCUSED}
	send(h, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&st)))
	send(h, LVM_ENSUREVISIBLE, uintptr(i), 0)
}

// ---------- fonts and measuring ----------

func (a *App) makeFonts() {
	if a.font != 0 {
		pDeleteObject.Call(a.font)
		pDeleteObject.Call(a.bold)
	}
	mk := func(pt, weight int) uintptr {
		h := -(pt * a.dpi / 72)
		f, _, _ := pCreateFontW.Call(uintptr(int32(h)), 0, 0, 0, uintptr(weight), 0, 0, 0, DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, 0,
			uintptr(unsafe.Pointer(u16("Malgun Gothic"))))
		return f
	}
	a.font = mk(9, FW_NORMAL)
	a.bold = mk(13, FW_BOLD)
}

func (a *App) textW(s string, font uintptr) int {
	dc, _, _ := pGetDC.Call(a.hwnd)
	defer pReleaseDC.Call(a.hwnd, dc)
	old, _, _ := pSelectObject.Call(dc, font)
	defer pSelectObject.Call(dc, old)
	s = strings.ReplaceAll(s, "&&", "&")
	var sz SIZE
	a16 := []uint16{}
	for _, r := range s {
		if r >= 0x10000 {
			r1, r2 := 0xD800+((r-0x10000)>>10), 0xDC00+((r-0x10000)&0x3FF)
			a16 = append(a16, uint16(r1), uint16(r2))
		} else {
			a16 = append(a16, uint16(r))
		}
	}
	if len(a16) == 0 {
		return 0
	}
	pGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&a16[0])), uintptr(len(a16)), uintptr(unsafe.Pointer(&sz)))
	return int(sz.CX)
}

// keyW is the width of the longer of the Korean and English text.
func (a *App) keyW(key string) int {
	w := 0
	for _, s := range Both(key) {
		if x := a.textW(s, a.font); x > w {
			w = x
		}
	}
	return w
}

// wrapH measures the height of wrapped text at a given width.
func (a *App) wrapH(s string, width int) int {
	if s == "" {
		return a.s(20)
	}
	dc, _, _ := pGetDC.Call(a.hwnd)
	defer pReleaseDC.Call(a.hwnd, dc)
	old, _, _ := pSelectObject.Call(dc, a.font)
	defer pSelectObject.Call(dc, old)
	r := RECT{0, 0, int32(width), 0}
	pDrawTextW.Call(dc, uintptr(unsafe.Pointer(u16(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), DT_CALCRECT|DT_WORDBREAK|DT_NOPREFIX|DT_EDITCONTROL)
	return int(r.Bottom) + a.s(2)
}

// maxWrapH is the taller of both languages for a text key.
func (a *App) maxWrapH(key string, width int) int {
	h := 0
	for _, s := range Both(key) {
		if x := a.wrapH(s, width); x > h {
			h = x
		}
	}
	return h
}

func (a *App) btnW(key string) int { return a.keyW(key) + a.s(28) }

// checkW: box + gap + text.
func (a *App) checkW(key string) int { return a.keyW(key) + a.s(28) }

// ---------- window ----------

func runApp() {
	runtime.LockOSThread()
	app = &App{byID: map[uint32]func(uint32){}, selRepo: -1, relIdx: -1, page: pgRelease, nextID: 1000}
	a := app

	exe, err := os.Executable()
	if err != nil {
		msgBox(0, err.Error(), "GitHub Relay", MB_ICONERROR)
		return
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	dataPath := filepath.Join(filepath.Dir(exe), "GithubRelay.dat")

	lang := LangEN
	if l, _, _ := pGetUserDefaultUILanguage.Call(); l&0x3FF == 0x12 {
		lang = LangKO
	}

	// One instance per data file, so two windows never overwrite each other.
	mname := "Local\\GitHubRelay-" + fmt.Sprintf("%08x", hashStr(strings.ToLower(dataPath)))
	if _, _, e := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16(mname)))); e == syscall.Errno(183) {
		msgBox(0, T(lang, "msg.already_running"), "GitHub Relay", MB_ICONINFORMATION)
		return
	}

	st, err := LoadStore(dataPath, dpapi{})
	if err != nil {
		msgBox(0, T(lang, "msg.load_fail", err.Error()), "GitHub Relay", MB_ICONERROR)
		return
	}
	a.store = st
	if st.D.Language == LangKO || st.D.Language == LangEN {
		lang = st.D.Language
	}
	a.lang = lang

	icc := INITCOMMONCONTROLSEX{DwICC: 0x1 | 0x8 | 0x20 | 0x4000}
	icc.DwSize = uint32(unsafe.Sizeof(icc))
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	a.hinst, _, _ = pGetModuleHandleW.Call(0)
	cls := u16("GitHubRelayWindow")
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	icon, _, _ := pLoadIconW.Call(a.hinst, 1) // resource id 1 from the .syso
	bg, _, _ := pGetSysColorBrush.Call(COLOR_WINDOW)
	wc := WNDCLASSEXW{LpfnWndProc: syscall.NewCallback(wndProc), HInstance: a.hinst, HCursor: cursor, HIcon: icon, HIconSm: icon,
		HbrBackground: bg, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	a.dpi = 96
	if pGetDpiForSystem.Find() == nil {
		d, _, _ := pGetDpiForSystem.Call()
		if d > 0 {
			a.dpi = int(d)
		}
	}
	w, h := a.s(1060), a.s(780)
	a.hwnd, _, _ = pCreateWindowExW.Call(WS_EX_ACCEPTFILES|WS_EX_CONTROLPARENT, uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(u16("GitHub Relay "+AppVersion))), WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN,
		CW_USEDEFAULT, CW_USEDEFAULT, uintptr(w), uintptr(h), 0, 0, a.hinst, 0)
	if a.hwnd == 0 {
		msgBox(0, "CreateWindow failed", "GitHub Relay", MB_ICONERROR)
		return
	}
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(a.hwnd); d > 0 && int(d) != a.dpi {
			a.dpi = int(d)
			pSetWindowPos.Call(a.hwnd, 0, 0, 0, uintptr(a.s(1060)), uintptr(a.s(780)), 0x2|SWP_NOZORDER|SWP_NOACTIVATE) // SWP_NOMOVE
		}
	}
	a.makeFonts()
	a.build()
	pDragAcceptFiles.Call(a.hwnd, 1)
	a.applyLanguage()
	a.refreshAll()
	a.renderPlan()
	a.showPage(pgRelease)
	pShowWindow.Call(a.hwnd, SW_SHOW)
	pUpdateWindow.Call(a.hwnd)
	a.post(a.checkExpiry)

	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if r, _, _ := pIsDialogMessageW.Call(a.hwnd, uintptr(unsafe.Pointer(&m))); r != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func hashStr(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	a := app
	switch msg {
	case wmRun:
		a.drain()
		return 0
	case WM_SIZE:
		if a.tab != 0 {
			a.layout()
		}
		return 0
	case WM_GETMINMAXINFO:
		if a != nil && a.dpi > 0 {
			mmi := (*[10]int32)(unsafe.Pointer(lp))
			mmi[6] = int32(a.s(960))
			mmi[7] = int32(a.s(720))
		}
		return 0
	case WM_DPICHANGED:
		a.dpi = int(loword(wp))
		r := (*RECT)(unsafe.Pointer(lp))
		a.makeFonts()
		for _, c := range a.ctrls {
			send(c.h, WM_SETFONT, a.font, 0)
		}
		send(a.title, WM_SETFONT, a.bold, 0)
		pSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), SWP_NOZORDER|SWP_NOACTIVATE)
		a.layout()
		redrawAll(hwnd)
		return 0
	case WM_COMMAND:
		if lp != 0 {
			if f, ok := a.byID[loword(wp)]; ok {
				f(hiword(wp))
			}
		}
		return 0
	case WM_NOTIFY:
		a.onNotify((*NMHDR)(unsafe.Pointer(lp)), lp)
		return 0
	case WM_DROPFILES:
		a.onDrop(wp)
		return 0
	case WM_CTLCOLORSTATIC:
		pSetBkMode.Call(wp, TRANSPARENT)
		if lp == a.disclaimer {
			c, _, _ := pGetSysColor.Call(COLOR_GRAYTEXT)
			pSetTextColor.Call(wp, c)
		}
		br, _, _ := pGetSysColorBrush.Call(COLOR_WINDOW)
		return br
	case WM_CLOSE:
		if a.uploading {
			if msgBox(hwnd, a.t("msg.quit_busy"), "GitHub Relay", MB_YESNO|MB_ICONWARNING) != IDYES {
				return 0
			}
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// ---------- building ----------

func (a *App) build() {
	a.title = a.create("STATIC", SS_LEFT|SS_NOPREFIX|SS_CENTERIMAGE, 0, pgAll, "", nil)
	setText(a.title, "GitHub Relay "+AppVersion)
	send(a.title, WM_SETFONT, a.bold, 1)
	a.radKO = a.create("BUTTON", WS_TABSTOP|WS_GROUP|BS_AUTORADIOBUTTON, 0, pgAll, "", func(c uint32) {
		if c == BN_CLICKED {
			a.setLang(LangKO)
		}
	})
	setText(a.radKO, "한국어")
	a.radEN = a.create("BUTTON", BS_AUTORADIOBUTTON, 0, pgAll, "", func(c uint32) {
		if c == BN_CLICKED {
			a.setLang(LangEN)
		}
	})
	setText(a.radEN, "English")

	a.tab = a.create("SysTabControl32", WS_TABSTOP|WS_CLIPSIBLINGS|WS_GROUP, 0, pgAll, "", nil)
	for i, k := range tabKeys {
		it := TCITEMW{Mask: TCIF_TEXT, PszText: u16(a.t(k))}
		send(a.tab, TCM_INSERTITEMW, uintptr(i), uintptr(unsafe.Pointer(&it)))
	}

	// Repositories
	a.lvRepos = a.listView(pgRepos, []string{"col.repo", "col.branch", "col.token", "col.latest", "col.expires"})
	a.lblURL = a.label(pgRepos, "lbl.url")
	a.edURL = a.edit(pgRepos, 0, "cue.url")
	a.lblTok = a.label(pgRepos, "lbl.token")
	a.edTok = a.edit(pgRepos, ES_PASSWORD, "cue.token")
	a.lblBr = a.label(pgRepos, "lbl.branch")
	a.edBr = a.edit(pgRepos, 0, "cue.branch")
	a.btnRegister = a.button(pgRepos, "btn.register", a.onRegister)
	a.btnReplace = a.button(pgRepos, "btn.replace", a.onReplace)
	a.btnVerify = a.button(pgRepos, "btn.verify", a.onVerify)
	a.btnRemove = a.button(pgRepos, "btn.remove", a.onRemove)
	a.btnEditAddr = a.button(pgRepos, "btn.edit_addr", a.onEditAddress)
	a.btnOpenRepo = a.button(pgRepos, "btn.openrepo", func() {
		if r := a.selectedRepo(); r != nil {
			openURL(r.URL())
		}
	})
	a.btnTokenPage = a.button(pgRepos, "btn.tokenpage", func() { openURL("https://github.com/settings/personal-access-tokens/new") })
	a.guide = a.wrapLabel(pgRepos, "guide.token")
	a.edResult = a.create("EDIT", ES_MULTILINE|ES_READONLY|ES_AUTOVSCROLL|WS_VSCROLL, WS_EX_CLIENTEDGE, pgRepos, "", nil)

	// Release
	a.lblRelRepo = a.label(pgRelease, "lbl.repo")
	a.lvRelRepos = a.listView(pgRelease, []string{"col.repo", "col.latest", "col.token"})
	a.summary = a.wrapLabel(pgRelease, "")
	a.dropFrame = a.create("STATIC", SS_LEFT|WS_CLIPSIBLINGS, WS_EX_CLIENTEDGE, pgRelease, "", nil)
	a.dropMsg = a.create("STATIC", SS_CENTER|SS_NOPREFIX, 0, pgRelease, "", nil)
	a.btnBrowse = a.button(pgRelease, "", a.onBrowse)
	a.btnReset = a.button(pgRelease, "btn.reset", a.resetRelease)
	a.btnReanalyze = a.button(pgRelease, "btn.reanalyze", func() {
		if a.bundlePath != "" {
			a.analyze()
		}
	})
	a.lvChecks = a.listView(pgRelease, []string{"col.result", "col.check"})
	a.lvChanges = a.listView(pgRelease, []string{"col.change", "col.path"})
	a.ckDraft = a.check(pgRelease, "chk.draft", a.updateUpload)
	a.ckApproveWF = a.check(pgRelease, "chk.approve_wf", a.updateUpload)
	a.ckApproveWarn = a.check(pgRelease, "chk.approve_warn", a.updateUpload)
	a.status = a.wrapLabel(pgRelease, "")
	a.progress = a.create("msctls_progress32", 0, 0, pgRelease, "", nil)
	send(a.progress, PBM_SETRANGE32, 0, 1000)
	a.btnOpenRel = a.button(pgRelease, "btn.openrelease", func() { openURL(a.lastRelURL) })
	a.btnUpload = a.button(pgRelease, "btn.upload", a.onUpload)
	// The drop message and its button sit on top of the drop frame.
	for _, h := range []uintptr{a.dropMsg, a.btnBrowse} {
		pSetWindowPos.Call(h, 0 /*HWND_TOP*/, 0, 0, 0, 0, 0x1|0x2|SWP_NOACTIVATE)
	}

	// History
	a.lblHistRepo = a.label(pgHistory, "lbl.repo")
	a.cbHistRepo = a.combo(pgHistory, a.refreshHistory)
	a.btnSync = a.button(pgHistory, "btn.sync", a.onSync)
	a.btnHistOpen = a.button(pgHistory, "btn.openrelease", func() {
		if h := a.selectedHist(); h != nil {
			openURL(h.ReleaseURL)
		}
	})
	a.btnHistCommit = a.button(pgHistory, "btn.opencommit", func() {
		if h := a.selectedHist(); h != nil {
			openURL(h.CommitURL)
		}
	})
	a.btnRetry = a.button(pgHistory, "btn.retry", a.onRetry)
	a.lvHist = a.listView(pgHistory, []string{"col.time", "col.version", "col.status", "col.changes", "col.branch", "col.note"})
	a.histStatus = a.wrapLabel(pgHistory, "")

	// Spec
	a.lblSpecRepo = a.label(pgSpec, "lbl.spec_repo")
	a.cbSpecRepo = a.combo(pgSpec, func() { a.specPicked = true; a.refreshSpec() })
	a.btnCopy = a.button(pgSpec, "btn.copy", a.onCopySpec)
	a.btnSaveMD = a.button(pgSpec, "btn.save_md", a.onSaveSpec)
	a.edSpec = a.create("EDIT", WS_TABSTOP|ES_MULTILINE|ES_READONLY|ES_AUTOVSCROLL|WS_VSCROLL, WS_EX_CLIENTEDGE, pgSpec, "", nil)
	send(a.edSpec, EM_SETLIMITTEXT, 0, 0)

	// Footer
	a.disclaimer = a.create("STATIC", SS_LEFT|SS_NOPREFIX|SS_CENTERIMAGE|SS_ENDELLIPSIS, 0, pgAll, "app.disclaimer", nil)
	a.btnAbout = a.button(pgAll, "btn.about", a.onAbout)

	// The tab control must be below (created last, so moved here) every page control, or it paints over them.
	pSetWindowPos.Call(a.tab, 1 /*HWND_BOTTOM*/, 0, 0, 0, 0, 0x1|0x2|SWP_NOACTIVATE)
}

func (a *App) showPage(p int) {
	a.page = p
	send(a.tab, TCM_SETCURSEL, uintptr(p), 0)
	for _, c := range a.ctrls {
		if c.page == pgAll {
			show(c.h, true)
		} else {
			show(c.h, c.page == p)
		}
	}
	a.updateDropState()
	a.layout()
}

func (a *App) onNotify(h *NMHDR, lp uintptr) {
	switch {
	case h.HwndFrom == a.tab && h.Code == TCN_SELCHANGE:
		p := int(send(a.tab, TCM_GETCURSEL, 0, 0))
		a.showPage(p)
	case h.HwndFrom == a.lvRelRepos && h.Code == LVN_ITEMCHANGED:
		nm := (*NMLISTVIEW)(unsafe.Pointer(lp))
		if nm.UNewState&LVIS_SELECTED != 0 {
			a.onRelRepoSelected(int(nm.IItem))
		}
	case h.HwndFrom == a.lvRepos && h.Code == LVN_ITEMCHANGED:
		nm := (*NMLISTVIEW)(unsafe.Pointer(lp))
		if nm.UNewState&LVIS_SELECTED != 0 {
			a.selRepo = int(nm.IItem)
			if r := a.selectedRepo(); r != nil {
				setText(a.edURL, r.URL())
				setText(a.edBr, r.Branch)
				setText(a.edTok, "")
			}
		}
		a.updateRepoButtons()
	case h.HwndFrom == a.lvHist && h.Code == LVN_ITEMCHANGED:
		a.updateHistButtons()
	case h.HwndFrom == a.lvHist && h.Code == NM_DBLCLK:
		if e := a.selectedHist(); e != nil {
			if e.ReleaseURL != "" {
				openURL(e.ReleaseURL)
			} else {
				openURL(e.CommitURL)
			}
		}
	}
}

// ---------- language ----------

func (a *App) setLang(l string) {
	if a.lang == l {
		return
	}
	a.lang = l
	a.store.D.Language = l
	a.save()
	a.applyLanguage()
	a.refreshAll()
	a.renderPlan()
}

func (a *App) applyLanguage() {
	setChecked(a.radKO, a.lang == LangKO)
	setChecked(a.radEN, a.lang == LangEN)
	for _, c := range a.ctrls {
		if c.textKey != "" {
			setText(c.h, a.t(c.textKey))
		}
		if c.cueKey != "" {
			send(c.h, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(u16(a.t(c.cueKey)))))
		}
	}
	for i, k := range tabKeys {
		it := TCITEMW{Mask: TCIF_TEXT, PszText: u16(a.t(k))}
		send(a.tab, TCM_SETITEMW, uintptr(i), uintptr(unsafe.Pointer(&it)))
	}
	for h, cols := range lvCols {
		for i, k := range cols {
			col := LVCOLUMNW{Mask: LVCF_TEXT, PszText: u16(a.t(k))}
			send(h, LVM_SETCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&col)))
		}
	}
	if a.statusKey != "" {
		setText(a.status, a.t(a.statusKey, a.statusArgs...))
	}
	if a.resultFn != nil {
		setText(a.edResult, a.resultFn())
	}
	if a.histFn != nil {
		setText(a.histStatus, a.histFn())
	}
	a.layout()
	redrawAll(a.hwnd)
}

// ---------- layout ----------

type item struct {
	h uintptr
	w int
}

// flow places items left to right, wrapping to new lines; returns the bottom y.
func (a *App) flow(items []item, x0, y, right, rowH int) int {
	gap := a.s(8)
	x := x0
	for _, it := range items {
		if x > x0 && x+it.w > right {
			x = x0
			y += rowH + gap
		}
		move(it.h, x, y, it.w, rowH)
		x += it.w + gap
	}
	return y + rowH
}

func (a *App) setCols(h uintptr, width int, fracs []float64) {
	cols := lvCols[h]
	width -= a.s(24) // vertical scrollbar room
	for i, f := range fracs {
		w := int(float64(width) * f)
		if m := a.keyW(cols[i]) + a.s(20); w < m {
			w = m
		}
		send(h, LVM_SETCOLUMNWIDTH, uintptr(i), uintptr(w))
	}
}

func (a *App) layout() {
	var rc RECT
	pGetClientRect.Call(a.hwnd, uintptr(unsafe.Pointer(&rc)))
	W, H := int(rc.Right), int(rc.Bottom)
	m := a.s(12)
	rowH := a.s(28)
	gap := a.s(8)

	// Header
	hdrH := a.s(34)
	move(a.title, m, m, a.textW("GitHub Relay "+AppVersion, a.bold)+a.s(10), hdrH)
	enW := a.textW("English", a.font) + a.s(30)
	koW := a.textW("한국어", a.font) + a.s(30)
	move(a.radEN, W-m-enW, m, enW, hdrH)
	move(a.radKO, W-m-enW-gap-koW, m, koW, hdrH)

	// Footer
	footH := a.s(28)
	aboutW := a.btnW("btn.about")
	fy := H - m - footH
	move(a.btnAbout, W-m-aboutW, fy, aboutW, footH)
	move(a.disclaimer, m, fy, W-3*m-aboutW, footH)

	// Tab
	ty := m + hdrH + a.s(6)
	tabH := fy - gap - ty
	move(a.tab, m, ty, W-2*m, tabH)
	pr := RECT{int32(m), int32(ty), int32(W - m), int32(ty + tabH)}
	send(a.tab, TCM_ADJUSTRECT, 0, uintptr(unsafe.Pointer(&pr)))
	pad := a.s(10)
	x0, y0, x1, y1 := int(pr.Left)+pad, int(pr.Top)+pad, int(pr.Right)-pad, int(pr.Bottom)-pad
	pw := x1 - x0

	switch a.page {
	case pgRepos:
		lblW := 0
		for _, k := range []string{"lbl.url", "lbl.token", "lbl.branch"} {
			if w := a.keyW(k); w > lblW {
				lblW = w
			}
		}
		lblW += a.s(12)
		// bottom-up: result box, guide, buttons, form, then the list takes the rest
		resultH := a.s(84)
		guideH := a.maxWrapH("guide.token", pw)
		btns := []item{}
		for _, b := range []struct {
			h uintptr
			k string
		}{{a.btnRegister, "btn.register"}, {a.btnReplace, "btn.replace"}, {a.btnVerify, "btn.verify"}, {a.btnEditAddr, "btn.edit_addr"}, {a.btnRemove, "btn.remove"}, {a.btnOpenRepo, "btn.openrepo"}, {a.btnTokenPage, "btn.tokenpage"}} {
			btns = append(btns, item{b.h, a.btnW(b.k)})
		}
		// measure button rows by dry-run
		lines := 1
		x := 0
		for _, b := range btns {
			if x > 0 && x+b.w > pw {
				lines++
				x = 0
			}
			x += b.w + gap
		}
		btnH := lines*rowH + (lines-1)*gap
		formH := 3*rowH + 2*gap
		listH := (y1 - y0) - (formH + btnH + guideH + resultH + 4*gap)
		if listH < a.s(90) {
			listH = a.s(90)
		}
		y := y0
		move(a.lvRepos, x0, y, pw, listH)
		a.setCols(a.lvRepos, pw, []float64{0.30, 0.12, 0.26, 0.14, 0.18})
		y += listH + gap
		for _, row := range [][2]uintptr{{a.lblURL, a.edURL}, {a.lblTok, a.edTok}, {a.lblBr, a.edBr}} {
			move(row[0], x0, y, lblW, rowH)
			move(row[1], x0+lblW, y, pw-lblW, rowH)
			y += rowH + gap
		}
		y = a.flow(btns, x0, y, x1, rowH) + gap
		move(a.guide, x0, y, pw, guideH)
		y += guideH + gap
		move(a.edResult, x0, y, pw, y1-y)

	case pgRelease:
		lblW := a.keyW("lbl.repo") + a.s(12)
		y := y0
		repoH := a.s(100)
		move(a.lblRelRepo, x0, y, lblW, rowH)
		move(a.lvRelRepos, x0+lblW, y, pw-lblW, repoH)
		a.setCols(a.lvRelRepos, pw-lblW, []float64{0.50, 0.16, 0.34})
		y += repoH + gap
		sumH := a.s(36)
		move(a.summary, x0, y, pw, sumH)
		y += sumH

		// bottom-up
		upW, orW := a.btnW("btn.upload"), a.btnW("btn.openrelease")
		by := y1 - rowH
		move(a.btnUpload, x1-upW, by, upW, rowH)
		move(a.btnOpenRel, x1-upW-gap-orW, by, orW, rowH)
		move(a.progress, x0, by+a.s(6), pw-upW-orW-3*gap, rowH-a.s(12))
		stH := a.s(40)
		sy := by - gap - stH
		move(a.status, x0, sy, pw, stH)
		ckH := a.s(24)
		cy := sy - gap - 3*ckH - 2*a.s(2)
		for i, c := range []struct {
			h uintptr
			k string
		}{{a.ckDraft, "chk.draft"}, {a.ckApproveWF, "chk.approve_wf"}, {a.ckApproveWarn, "chk.approve_warn"}} {
			w := a.checkW(c.k)
			if w > pw {
				w = pw
			}
			move(c.h, x0, cy+i*(ckH+a.s(2)), w, ckH)
		}
		lw := pw * 55 / 100
		ay := cy - gap - rowH
		move(a.btnReset, x0, ay, a.btnW("btn.reset"), rowH)
		move(a.btnReanalyze, x0+lw+gap, ay, a.btnW("btn.reanalyze"), rowH)
		panelH := ay - gap - y
		move(a.lvChecks, x0, y, lw, panelH)
		a.setCols(a.lvChecks, lw, []float64{0.16, 0.84})
		move(a.lvChanges, x0+lw+gap, y, pw-lw-gap, panelH)
		a.setCols(a.lvChanges, pw-lw-gap, []float64{0.22, 0.78})
		// drop area: message and button centred in the left panel
		move(a.dropFrame, x0, y, lw, panelH)
		msgW := lw - a.s(40)
		msgH := a.maxWrapH("drop.hint", msgW)
		if h := a.maxWrapH("drop.need_repo", msgW); h > msgH {
			msgH = h
		}
		bw := a.btnW("btn.browse")
		if w := a.btnW("btn.goto_repos"); w > bw {
			bw = w
		}
		my := y + (panelH-msgH-a.s(10)-rowH)/2
		move(a.dropMsg, x0+a.s(20), my, msgW, msgH)
		move(a.btnBrowse, x0+(lw-bw)/2, my+msgH+a.s(10), bw, rowH)

	case pgHistory:
		lblW := a.keyW("lbl.repo") + a.s(12)
		cbW := a.s(320)
		items := []item{{a.cbHistRepo, cbW}, {a.btnSync, a.btnW("btn.sync")}, {a.btnHistOpen, a.btnW("btn.openrelease")}, {a.btnHistCommit, a.btnW("btn.opencommit")}, {a.btnRetry, a.btnW("btn.retry")}}
		move(a.lblHistRepo, x0, y0, lblW, rowH)
		y := a.flow(items, x0+lblW, y0, x1, rowH) + gap
		move(a.cbHistRepo, x0+lblW, y0, cbW, a.s(300)) // combo needs its dropdown height
		stH := a.s(40)
		move(a.histStatus, x0, y1-stH, pw, stH)
		move(a.lvHist, x0, y, pw, y1-stH-gap-y)
		a.setCols(a.lvHist, pw, []float64{0.17, 0.09, 0.19, 0.17, 0.10, 0.28})

	case pgSpec:
		lblW := a.keyW("lbl.spec_repo") + a.s(12)
		cbW := a.s(320)
		items := []item{{a.cbSpecRepo, cbW}, {a.btnCopy, a.btnW("btn.copy")}, {a.btnSaveMD, a.btnW("btn.save_md")}}
		move(a.lblSpecRepo, x0, y0, lblW, rowH)
		y := a.flow(items, x0+lblW, y0, x1, rowH) + gap
		move(a.cbSpecRepo, x0+lblW, y0, cbW, a.s(300))
		move(a.edSpec, x0, y, pw, y1-y)
	}
	redrawAll(a.hwnd)
}

// ---------- helpers ----------

func (a *App) save() bool {
	if err := a.store.Save(); err != nil {
		msgBox(a.hwnd, a.t("msg.save_fail", err.Error()), "GitHub Relay", MB_ICONERROR)
		return false
	}
	return true
}

func (a *App) info(s string) { msgBox(a.hwnd, s, "GitHub Relay", MB_ICONINFORMATION) }
func (a *App) warn(s string) { msgBox(a.hwnd, s, "GitHub Relay", MB_ICONWARNING) }
func (a *App) ask(s string) bool {
	return msgBox(a.hwnd, s, "GitHub Relay", MB_YESNO|MB_ICONQUESTION) == IDYES
}

func (a *App) setBusy(b bool) {
	a.busy = b
	for _, h := range []uintptr{a.btnRegister, a.btnReplace, a.btnVerify, a.btnEditAddr, a.btnRemove, a.btnBrowse, a.btnReanalyze, a.btnReset, a.btnSync, a.btnRetry, a.lvRelRepos, a.radKO, a.radEN} {
		enable(h, !b)
	}
	if !b {
		a.updateRepoButtons()
		a.updateHistButtons()
	}
	a.updateDropState()
	a.updateUpload()
}

func (a *App) selectedRepo() *RepoEntry {
	if a.selRepo >= 0 && a.selRepo < len(a.store.D.Repos) {
		return a.store.D.Repos[a.selRepo]
	}
	return nil
}

func comboRepo(a *App, cb uintptr, offset int) *RepoEntry {
	i := int(int32(send(cb, CB_GETCURSEL, 0, 0))) - offset
	if i >= 0 && i < len(a.store.D.Repos) {
		return a.store.D.Repos[i]
	}
	return nil
}

func (a *App) levelText(l Level) string {
	switch l {
	case Pass:
		return "✔ " + a.t("lvl.pass")
	case Warn:
		return "⚠ " + a.t("lvl.warn")
	}
	return "✖ " + a.t("lvl.fail")
}

func (a *App) checksText(cs []Check) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(a.levelText(c.Level) + "  " + c.Text(a.lang) + "\r\n")
	}
	return b.String()
}

func (a *App) statusText(r *RepoEntry) (string, string) {
	st, exp := a.store.Status(r, time.Now())
	key := map[TokenStatus]string{TSOk: "ts.ok", TSExpiring: "ts.expiring", TSExpired: "ts.expired", TSNoAccess: "ts.noaccess",
		TSNoWrite: "ts.nowrite", TSNeedToken: "ts.needtok", TSUnknown: "ts.unknown"}[st]
	e := a.t("none")
	if !exp.IsZero() {
		e = exp.Local().Format("2006-01-02")
	}
	return a.t(key) + "  ·  " + shortHint(r.TokenHint), e
}

// ---------- refresh ----------

func (a *App) refreshAll() {
	a.refreshRepos()
	a.refreshCombos()
	a.refreshHistory()
	a.refreshSpec()
}

func (a *App) refreshRepos() {
	lvClear(a.lvRepos)
	for _, r := range a.store.D.Repos {
		tok, exp := a.statusText(r)
		br := r.Branch
		if br == "" {
			br = a.t("default")
		}
		latest := a.t("none")
		if h := r.LatestSuccess(); h != nil {
			latest = h.Version
		}
		lvAdd(a.lvRepos, r.Full(), br, tok, latest, exp)
	}
	if a.selRepo >= len(a.store.D.Repos) {
		a.selRepo = -1
	}
	if a.selRepo >= 0 {
		lvSelect(a.lvRepos, a.selRepo)
	}
	a.updateRepoButtons()
}

func (a *App) updateRepoButtons() {
	has := a.selectedRepo() != nil && !a.busy
	for _, h := range []uintptr{a.btnReplace, a.btnVerify, a.btnEditAddr, a.btnRemove, a.btnOpenRepo} {
		enable(h, has)
	}
	enable(a.btnRegister, !a.busy)
}

func (a *App) refreshCombos() {
	fill := func(cb uintptr, withNone bool) {
		cur := int(int32(send(cb, CB_GETCURSEL, 0, 0)))
		send(cb, CB_RESETCONTENT, 0, 0)
		if withNone {
			send(cb, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(u16(a.t("spec.none")))))
		}
		for _, r := range a.store.D.Repos {
			send(cb, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(u16(r.Full()))))
		}
		n := len(a.store.D.Repos)
		if withNone {
			n++
		}
		if cur < 0 || cur >= n {
			cur = 0
		}
		if withNone && !a.specPicked && len(a.store.D.Repos) > 0 {
			cur = 1 // default to the first registered repository
		}
		if n > 0 {
			send(cb, CB_SETCURSEL, uintptr(cur), 0)
		}
	}
	a.refreshRelRepos()
	fill(a.cbHistRepo, false)
	fill(a.cbSpecRepo, true)
	a.updateDropState()
}

func (a *App) histStatusText(s string) string {
	return a.t("stt." + s)
}

func (a *App) refreshHistory() {
	lvClear(a.lvHist)
	a.histRows = nil
	r := comboRepo(a, a.cbHistRepo, 0)
	if r == nil {
		a.updateHistButtons()
		return
	}
	for _, h := range r.History {
		t := h.Time
		if tm, err := time.Parse(time.RFC3339, h.Time); err == nil {
			t = tm.Local().Format("2006-01-02 15:04")
		}
		ch := fmt.Sprintf("+%d  ~%d  -%d", h.Added, h.Modified, h.Deleted)
		if h.Status == StExternal {
			ch = a.t("none")
		}
		br := h.Branch
		if br == "" {
			br = a.t("none")
		}
		lvAdd(a.lvHist, t, h.Version, a.histStatusText(h.Status), ch, br, h.ErrorText(a.lang))
		a.histRows = append(a.histRows, h)
	}
	if len(r.History) == 0 {
		a.setHist(func() string { return a.t("hist.empty") })
	} else {
		a.setHist(func() string { return "" })
	}
	a.updateHistButtons()
}

func (a *App) selectedHist() *HistoryEntry {
	i := lvSelected(a.lvHist)
	if i >= 0 && i < len(a.histRows) {
		return a.histRows[i]
	}
	return nil
}

func (a *App) updateHistButtons() {
	h := a.selectedHist()
	enable(a.btnHistOpen, h != nil && h.ReleaseURL != "")
	enable(a.btnHistCommit, h != nil && h.CommitURL != "")
	enable(a.btnRetry, h != nil && h.Status == StCommitted && !a.busy)
	enable(a.btnSync, comboRepo(a, a.cbHistRepo, 0) != nil && !a.busy)
}

func (a *App) specText() string {
	return SpecMarkdown(a.lang, SpecFor(comboRepo(a, a.cbSpecRepo, 1)))
}

func (a *App) refreshSpec() {
	setText(a.edSpec, strings.ReplaceAll(a.specText(), "\n", "\r\n"))
}

// ---------- repositories actions ----------

func (a *App) readForm() (owner, name, token, branch string, ok bool) {
	owner, name, err := ParseRepo(getText(a.edURL))
	if err != nil {
		a.warn(a.t("msg.url_invalid"))
		return "", "", "", "", false
	}
	return owner, name, strings.TrimSpace(getText(a.edTok)), strings.TrimSpace(getText(a.edBr)), true
}

// verifyAsync runs VerifyToken in the background and calls done on the UI thread.
func (a *App) verifyAsync(owner, name, token string, done func(*RegisterResult)) {
	if a.busy {
		a.warn(a.t("msg.busy"))
		return
	}
	a.setBusy(true)
	a.setResult(func() string { return a.t("msg.verifying") })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res := VerifyToken(ctx, NewGitHub(token), owner, name)
		a.post(func() {
			a.setBusy(false)
			a.setResult(func() string { return a.checksText(res.Checks) })
			done(res)
		})
	}()
}

func applyVerify(r *RepoEntry, res *RegisterResult) {
	if res.Info != nil && res.Info.ID != 0 && r.RepoID == 0 {
		r.RepoID = res.Info.ID
	}
	r.TokenState = res.State
	r.LastCheck = now()
	if !res.Expires.IsZero() {
		r.TokenExpires = res.Expires.UTC().Format(time.RFC3339)
	} else if res.Info != nil {
		r.TokenExpires = ""
	}
}

func (a *App) onRegister() {
	owner, name, token, branch, ok := a.readForm()
	if !ok {
		return
	}
	if token == "" {
		a.warn(a.t("msg.token_empty"))
		return
	}
	if a.store.Find(owner+"/"+name) != nil {
		a.warn(a.t("msg.already"))
		return
	}
	a.verifyAsync(owner, name, token, func(res *RegisterResult) {
		if HasFail(res.Checks) {
			a.setResult(func() string { return a.checksText(res.Checks) + "\r\n" + a.t("msg.reg_fail") })
			return
		}
		if res.Info != nil && strings.Contains(res.Info.FullName, "/") {
			owner, name, _ = ParseRepo(res.Info.FullName)
		}
		r := &RepoEntry{Owner: owner, Name: name, Branch: branch, History: []*HistoryEntry{}}
		if err := a.store.SetToken(r, token); err != nil {
			a.warn(err.Error())
			return
		}
		applyVerify(r, res)
		if held := a.store.TakeArchived(r.Full()); len(held) > 0 {
			r.History = held
			a.addResult(func() string { return a.t("msg.held_history", len(held)) })
		}
		a.store.D.Repos = append(a.store.D.Repos, r)
		if !a.save() {
			return
		}
		setText(a.edTok, "")
		a.selRepo = len(a.store.D.Repos) - 1
		a.refreshAll()
		a.addResult(func() string { return a.t("msg.reg_ok", r.Full()) })
	})
}

func (a *App) onReplace() {
	r := a.selectedRepo()
	if r == nil {
		a.warn(a.t("msg.select_repo"))
		return
	}
	token := strings.TrimSpace(getText(a.edTok))
	if token == "" {
		a.warn(a.t("msg.token_empty"))
		return
	}
	a.verifyAsync(r.Owner, r.Name, token, func(res *RegisterResult) {
		if HasFail(res.Checks) {
			a.setResult(func() string { return a.checksText(res.Checks) + "\r\n" + a.t("msg.reg_fail") })
			return
		}
		if err := a.store.SetToken(r, token); err != nil {
			a.warn(err.Error())
			return
		}
		applyVerify(r, res)
		if b := strings.TrimSpace(getText(a.edBr)); b != r.Branch {
			r.Branch = b
		}
		a.offerRename(r, res)
		if a.save() {
			setText(a.edTok, "")
			a.refreshAll()
			a.addResult(func() string { return a.t("msg.replace_ok", r.Full()) })
		}
	})
}

func (a *App) onVerify() {
	r := a.selectedRepo()
	if r == nil {
		a.warn(a.t("msg.select_repo"))
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	a.verifyAsync(r.Owner, r.Name, token, func(res *RegisterResult) {
		applyVerify(r, res)
		if b := strings.TrimSpace(getText(a.edBr)); b != r.Branch {
			r.Branch = b
		}
		a.offerRename(r, res)
		if a.save() {
			a.refreshAll()
			a.addResult(func() string { return a.t("msg.verify_done", r.Full()) })
		}
	})
}

func (a *App) onRemove() {
	r := a.selectedRepo()
	if r == nil {
		return
	}
	if !a.ask(a.t("msg.remove_q", r.Full())) {
		return
	}
	switch msgBox(a.hwnd, a.t("msg.remove_hist_q"), "GitHub Relay", MB_YESNOCANCEL|MB_ICONQUESTION) {
	case IDYES:
	case IDNO:
		a.store.Archive(r)
	default:
		return
	}
	a.store.Remove(r)
	a.selRepo = -1
	a.save()
	setText(a.edURL, "")
	setText(a.edBr, "")
	setText(a.edTok, "")
	a.refreshAll()
	a.resetRelease()
}

// ---------- release actions ----------

func (a *App) clearPlan() {
	a.plan = nil
	a.planUsed = false
	setChecked(a.ckApproveWF, false)
	setChecked(a.ckApproveWarn, false)
}

func (a *App) setStatus(key string, args ...any) {
	a.statusKey, a.statusArgs = key, args
	if key == "" {
		setText(a.status, "")
		return
	}
	setText(a.status, a.t(key, args...))
}

func (a *App) onDrop(hdrop uintptr) {
	defer pDragFinish.Call(hdrop)
	n, _, _ := pDragQueryFileW.Call(hdrop, 0xFFFFFFFF, 0, 0)
	if n != 1 {
		a.warn(a.t("msg.only_zip"))
		return
	}
	buf := make([]uint16, 32768)
	pDragQueryFileW.Call(hdrop, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	p := syscall.UTF16ToString(buf)
	if !strings.EqualFold(filepath.Ext(p), ".zip") {
		a.warn(a.t("msg.only_zip"))
		return
	}
	if a.busy {
		a.warn(a.t("msg.busy"))
		return
	}
	a.showPage(pgRelease)
	a.loadBundle(p)
}

func (a *App) onBrowse() {
	if len(a.store.D.Repos) == 0 {
		a.showPage(pgRepos)
		return
	}
	p, ok := fileDialog(a.hwnd, false, a.t("filter.zip"), "*.zip", "", "zip")
	if !ok {
		return
	}
	a.loadBundle(p)
}

// loadBundle selects the repository the bundle names (if registered) and starts the check.
func (a *App) loadBundle(p string) {
	if len(a.store.D.Repos) == 0 {
		a.warn(a.t("drop.need_repo"))
		return
	}
	if repo := PeekRepo(p); repo != "" {
		for i, r := range a.store.D.Repos {
			if r.KnownAs(repo) && i != a.relIdx {
				a.autoSel = true
				lvSelect(a.lvRelRepos, i)
				a.autoSel = false
				break
			}
		}
	}
	a.bundlePath = p
	a.clearPlan()
	a.updateDropState()
	a.analyze()
}

// resetRelease forgets the bundle and returns to the drop area.
func (a *App) resetRelease() {
	if a.busy {
		return
	}
	a.bundlePath = ""
	a.clearPlan()
	a.setStatus("")
	send(a.progress, PBM_SETPOS, 0, 0)
	a.renderPlan()
	a.updateDropState()
}

func (a *App) relRepo() *RepoEntry {
	if a.relIdx >= 0 && a.relIdx < len(a.store.D.Repos) {
		return a.store.D.Repos[a.relIdx]
	}
	return nil
}

func (a *App) onRelRepoSelected(i int) {
	if i == a.relIdx {
		return
	}
	a.relIdx = i
	if r := a.relRepo(); r != nil && a.store.D.LastRepo != r.Full() {
		a.store.D.LastRepo = r.Full()
		a.save()
	}
	if !a.autoSel {
		a.resetRelease()
	}
}

func (a *App) refreshRelRepos() {
	lvClear(a.lvRelRepos)
	want := -1
	for i, r := range a.store.D.Repos {
		tok, _ := a.statusText(r)
		latest := a.t("none")
		if h := r.LatestSuccess(); h != nil {
			latest = h.Version
		}
		lvAdd(a.lvRelRepos, r.Full(), latest, tok)
		if SameRepo(r.Full(), a.store.D.LastRepo) {
			want = i
		}
	}
	if want < 0 && len(a.store.D.Repos) > 0 {
		want = 0
	}
	a.relIdx = want
	if want >= 0 {
		a.autoSel = true
		lvSelect(a.lvRelRepos, want)
		a.autoSel = false
	}
}

// updateDropState shows the drop area when no bundle is loaded, and the check list otherwise.
func (a *App) updateDropState() {
	loaded := a.bundlePath != ""
	on := a.page == pgRelease
	noRepo := len(a.store.D.Repos) == 0
	for _, h := range []uintptr{a.dropFrame, a.dropMsg, a.btnBrowse} {
		show(h, on && !loaded)
	}
	show(a.lvChecks, on && loaded)
	if noRepo {
		setText(a.dropMsg, a.t("drop.need_repo"))
		setText(a.btnBrowse, a.t("btn.goto_repos"))
	} else {
		setText(a.dropMsg, a.t("drop.hint"))
		setText(a.btnBrowse, a.t("btn.browse"))
	}
	enable(a.btnBrowse, !a.busy)
	enable(a.btnReset, loaded && !a.busy)
	enable(a.btnReanalyze, loaded && !a.busy && !noRepo)
}

func (a *App) bundleLabel() string {
	return a.t("drop.file", filepath.Base(a.bundlePath)) + "   ·   "
}

func (a *App) analyze() {
	r := a.relRepo()
	a.updateDropState()
	if r == nil || a.bundlePath == "" {
		a.renderPlan()
		return
	}
	if a.busy {
		a.warn(a.t("msg.busy"))
		return
	}
	token, err := a.store.Token(r)
	path := a.bundlePath
	a.clearPlan()
	a.setBusy(true)
	a.setStatus("st.analyzing")
	a.setMarquee(true)
	lvClear(a.lvChecks)
	lvClear(a.lvChanges)
	setText(a.summary, "")
	// Work on a copy so the background task never touches live data.
	cp := &RepoEntry{Owner: r.Owner, Name: r.Name, Branch: r.Branch, History: cloneHistory(r.History), FormerNames: append([]string(nil), r.FormerNames...)}
	go func() {
		b := LoadBundle(path, cp.Full(), cp.FormerNames...)
		var p *Plan
		if err != nil {
			p = &Plan{Bundle: b, Owner: cp.Owner, Name: cp.Name}
			p.add(Fail, "msg.need_token")
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			p = Analyze(ctx, NewGitHub(token), cp, b)
		}
		a.post(func() {
			a.setMarquee(false)
			a.setBusy(false)
			if path != a.bundlePath {
				return
			}
			a.plan = p
			a.renderPlan()
			if p.RepoID != 0 && r.RepoID == 0 {
				r.RepoID = p.RepoID
				a.save()
			}
			if p.RenamedTo != "" && a.ask(a.t("msg.renamed_q", r.Full(), p.RenamedTo)) {
				if a.applyRename(r, p.RenamedTo) {
					a.analyze()
				}
			}
		})
	}()
}

func (a *App) renderPlan() {
	lvClear(a.lvChecks)
	lvClear(a.lvChanges)
	p := a.plan
	if p == nil {
		if a.bundlePath != "" {
			setText(a.summary, a.bundleLabel())
		} else {
			setText(a.summary, a.t("summary.none"))
		}
		if a.bundlePath == "" {
			a.setStatus("")
		}
		a.updateUpload()
		return
	}
	for _, c := range p.AllChecks() {
		lvAdd(a.lvChecks, a.levelText(c.Level), c.Text(a.lang))
	}
	for _, c := range p.Changes {
		k := map[ChangeKind]string{ChAdded: "ch.added", ChModified: "ch.modified", ChDeleted: "ch.deleted"}[c.Kind]
		lvAdd(a.lvChanges, a.t(k), c.Path)
	}
	b := p.Bundle
	ver := b.Version.String()
	if ver == "" {
		ver = a.t("none")
	}
	tag := b.Tag
	if tag == "" {
		tag = a.t("none")
	}
	if p.Branch != "" {
		setText(a.summary, a.bundleLabel()+a.t("summary", ver, tag, p.Branch, p.Added, p.Modified, p.Deleted, p.Unchanged))
	} else {
		setText(a.summary, a.bundleLabel()+a.t("summary.local", ver, tag, len(b.Files), len(b.Assets)))
	}
	if !a.planUsed {
		switch {
		case !p.CanUpload():
			a.setStatus("st.blocked")
		default:
			a.setStatus("st.ready")
		}
	}
	a.updateUpload()
}

func (a *App) updateUpload() {
	p := a.plan
	needWF := p != nil && p.NeedsWorkflowApproval()
	needWarn := p != nil && p.NeedsWarnApproval()
	canUp := p != nil && p.CanUpload()
	enable(a.ckApproveWF, needWF && canUp && !a.busy && !a.planUsed)
	enable(a.ckApproveWarn, needWarn && canUp && !a.busy && !a.planUsed)
	enable(a.ckDraft, !a.busy && !a.planUsed)
	ok := p != nil && !a.busy && !a.planUsed && p.CanUpload() &&
		(!needWF || checked(a.ckApproveWF)) && (!needWarn || checked(a.ckApproveWarn))
	enable(a.btnUpload, ok)
	enable(a.btnOpenRel, a.lastRelURL != "")
	if p != nil && p.CanUpload() && !ok && !a.busy && !a.planUsed {
		a.setStatus("st.need_approval")
	} else if p != nil && p.CanUpload() && ok {
		a.setStatus("st.ready")
	}
}

func (a *App) onUpload() {
	p := a.plan
	r := a.relRepo()
	if p == nil || r == nil || !p.CanUpload() || a.busy || a.planUsed || !SameRepo(r.Full(), p.Owner+"/"+p.Name) {
		return
	}
	draft := checked(a.ckDraft)
	vis := a.t("vis.public")
	if draft {
		vis = a.t("vis.draft")
	}
	var q string
	if a.lang == LangKO {
		q = a.t("confirm.upload", r.Full(), p.Bundle.Version.String(), p.Branch, p.Added, p.Modified, p.Deleted, len(p.Bundle.Assets), vis)
	} else {
		q = a.t("confirm.upload", p.Bundle.Version.String(), r.Full(), p.Branch, p.Added, p.Modified, p.Deleted, len(p.Bundle.Assets), vis)
	}
	if !a.ask(q) {
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	a.planUsed = true
	a.uploading = true
	a.setBusy(true)
	a.setStatus("st.uploading")
	go func() {
		ctx := context.Background()
		h := Upload(ctx, NewGitHub(token), p, draft, func(done, total int, key string, args ...any) {
			a.post(func() {
				if total > 0 {
					send(a.progress, PBM_SETPOS, uintptr(done*1000/total), 0)
				}
				a.setStatus(key, args...)
			})
		})
		a.post(func() {
			a.uploading = false
			r.History = append([]*HistoryEntry{h}, r.History...)
			a.save()
			a.setBusy(false)
			switch h.Status {
			case StSuccess:
				a.lastRelURL = h.ReleaseURL
				send(a.progress, PBM_SETPOS, 1000, 0)
				a.setStatus("st.done", h.ReleaseURL)
			case StDraft:
				a.lastRelURL = h.ReleaseURL
				send(a.progress, PBM_SETPOS, 1000, 0)
				a.setStatus("st.done_draft")
			case StCommitted:
				a.setStatus("st.committed", h.ErrorText(a.lang))
			default:
				a.planUsed = true // nothing changed on GitHub; press "Check again" to retry
				a.setStatus("st.failed", h.ErrorText(a.lang))
			}
			a.refreshRepos()
			a.refreshHistory()
			a.refreshSpec()
			a.updateUpload()
		})
	}()
}

// ---------- history actions ----------

func cloneHistory(hs []*HistoryEntry) []*HistoryEntry {
	out := make([]*HistoryEntry, len(hs))
	for i, h := range hs {
		c := *h
		out[i] = &c
	}
	return out
}

func (a *App) onSync() {
	r := comboRepo(a, a.cbHistRepo, 0)
	if r == nil || a.busy {
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	cp := &RepoEntry{Owner: r.Owner, Name: r.Name, History: cloneHistory(r.History)}
	a.setBusy(true)
	a.setHist(func() string { return a.t("msg.verifying") })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := Sync(ctx, NewGitHub(token), cp)
		a.post(func() {
			a.setBusy(false)
			if err != nil {
				k, args := explainAPIError(err)
				a.refreshHistory()
				a.setHist(func() string { return a.t(k, args...) })
				return
			}
			r.History = cp.History
			a.save()
			a.refreshRepos()
			a.refreshHistory()
			a.refreshSpec()
			n := 0
			for _, h := range r.History {
				if h.ReleaseID != 0 && h.Status != StDeleted {
					n++
				}
			}
			a.setHist(func() string { return a.t("msg.synced", n) })
		})
	}()
}

func (a *App) onRetry() {
	r := comboRepo(a, a.cbHistRepo, 0)
	h := a.selectedHist()
	if r == nil || h == nil {
		a.warn(a.t("msg.select_entry"))
		return
	}
	if h.Status != StCommitted {
		a.info(a.t("msg.no_retry"))
		return
	}
	path := h.BundlePath
	if _, err := os.Stat(path); err != nil {
		a.warn(a.t("msg.retry_bundle_missing", path))
		p, ok := fileDialog(a.hwnd, false, a.t("filter.zip"), "*.zip", "", "zip")
		if !ok {
			return
		}
		path = p
	}
	if !a.ask(a.t("msg.retry_q", h.Version)) {
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	cp := *h
	a.setBusy(true)
	a.uploading = true
	a.setHist(func() string { return a.t("st.uploading") })
	go func() {
		b := LoadBundle(path, r.Full())
		RetryRelease(context.Background(), NewGitHub(token), r, &cp, b, func(int, int, string, ...any) {})
		a.post(func() {
			a.uploading = false
			a.setBusy(false)
			*h = cp
			h.BundlePath = path
			a.save()
			a.refreshRepos()
			a.refreshHistory()
			if h.Status == StCommitted {
				a.setHist(func() string { return a.t("st.failed", h.ErrorText(a.lang)) })
			} else {
				a.setHist(func() string { return a.t("st.done", h.ReleaseURL) })
			}
		})
	}()
}

// ---------- spec actions ----------

func (a *App) onCopySpec() {
	if comboRepo(a, a.cbSpecRepo, 1) == nil && !a.ask(a.t("msg.spec_no_repo")) {
		return
	}
	if setClipboard(a.hwnd, strings.ReplaceAll(a.specText(), "\n", "\r\n")) {
		a.info(a.t("msg.copied"))
	}
}

func (a *App) onSaveSpec() {
	if comboRepo(a, a.cbSpecRepo, 1) == nil && !a.ask(a.t("msg.spec_no_repo")) {
		return
	}
	name := "GitHubRelay-bundle-spec-" + a.lang + ".md"
	if r := comboRepo(a, a.cbSpecRepo, 1); r != nil {
		name = r.Name + "-bundle-spec-" + a.lang + ".md"
	}
	p, ok := fileDialog(a.hwnd, true, a.t("filter.md"), "*.md", name, "md")
	if !ok {
		return
	}
	if err := os.WriteFile(p, []byte(a.specText()), 0o644); err != nil {
		a.warn(err.Error())
		return
	}
	a.info(a.t("msg.saved", p))
}

func (a *App) onAbout() {
	if a.ask(a.t("about.text", AppVersion, ProjectURL, a.store.Path)) {
		openURL(ProjectURL)
	}
}

// shortHint drops the token prefix for display: "github_pat_…ab12" -> "…ab12".
func shortHint(h string) string {
	if i := strings.Index(h, "…"); i >= 0 {
		return h[i:]
	}
	return h
}

func (a *App) setResult(f func() string) {
	a.resultFn = f
	setText(a.edResult, f())
}

func (a *App) addResult(f func() string) {
	prev := a.resultFn
	if prev == nil {
		a.setResult(f)
		return
	}
	a.setResult(func() string { return prev() + "\r\n" + f() })
}

func (a *App) setHist(f func() string) {
	a.histFn = f
	setText(a.histStatus, f())
}

// setMarquee switches the progress bar between a moving "busy" bar and a normal bar.
func (a *App) setMarquee(on bool) {
	const pbsMarquee, pbmSetMarquee = 0x8, 0x40A
	gwlStyle := ^uintptr(15) // -16
	st, _, _ := pGetWindowLongPtrW.Call(a.progress, gwlStyle)
	if on {
		pSetWindowLongPtrW.Call(a.progress, gwlStyle, st|pbsMarquee)
		send(a.progress, pbmSetMarquee, 1, 30)
		return
	}
	send(a.progress, pbmSetMarquee, 0, 0)
	pSetWindowLongPtrW.Call(a.progress, gwlStyle, st&^pbsMarquee)
	send(a.progress, PBM_SETRANGE32, 0, 1000)
	send(a.progress, PBM_SETPOS, 0, 0)
}

// checkExpiry warns once at startup about tokens that expired or expire within 14 days.
func (a *App) checkExpiry() {
	var lines []string
	for _, r := range a.store.D.Repos {
		st, exp := a.store.Status(r, time.Now())
		if st != TSExpiring && st != TSExpired {
			continue
		}
		key := "ts.expiring"
		if st == TSExpired {
			key = "ts.expired"
		}
		lines = append(lines, fmt.Sprintf("• %s — %s (%s)", r.Full(), a.t(key), exp.Local().Format("2006-01-02")))
	}
	if len(lines) > 0 {
		a.warn(a.t("msg.expiry_notice", strings.Join(lines, "\n")))
	}
}

var tabKeys = []string{"tab.release", "tab.history", "tab.spec", "tab.repos"}

// offerRename asks to follow a rename that verification revealed.
func (a *App) offerRename(r *RepoEntry, res *RegisterResult) {
	if res.Info == nil || res.Info.FullName == "" || SameRepo(res.Info.FullName, r.Full()) {
		return
	}
	if a.ask(a.t("msg.renamed_q", r.Full(), res.Info.FullName)) {
		a.applyRename(r, res.Info.FullName)
	}
}

// applyRename changes the registered address, keeping token and history.
func (a *App) applyRename(r *RepoEntry, newFull string) bool {
	if other := a.store.Find(newFull); other != nil && other != r {
		a.warn(a.t("msg.addr_taken", newFull))
		return false
	}
	old := r.Full()
	if err := a.store.Rename(r, newFull); err != nil {
		a.warn(a.t("msg.url_invalid"))
		return false
	}
	if !a.save() {
		return false
	}
	a.refreshAll()
	if a.selectedRepo() == r {
		setText(a.edURL, r.URL())
	}
	nf := r.Full()
	a.addResult(func() string { return a.t("msg.renamed_done", old, nf) })
	return true
}

// onEditAddress changes the address of the selected registration to the URL in the form.
// Only the same repository (by GitHub id) is accepted, so history stays truthful.
func (a *App) onEditAddress() {
	r := a.selectedRepo()
	if r == nil {
		a.warn(a.t("msg.select_repo"))
		return
	}
	owner, name, err := ParseRepo(getText(a.edURL))
	if err != nil {
		a.warn(a.t("msg.url_invalid"))
		return
	}
	newFull := owner + "/" + name
	if newFull == r.Full() {
		a.info(a.t("msg.addr_same"))
		return
	}
	if other := a.store.Find(newFull); other != nil && other != r {
		a.warn(a.t("msg.addr_taken", newFull))
		return
	}
	token, err := a.store.Token(r)
	if err != nil {
		a.warn(a.t("msg.need_token"))
		return
	}
	a.verifyAsync(owner, name, token, func(res *RegisterResult) {
		if res.Info == nil {
			a.setResult(func() string { return a.checksText(res.Checks) + "\r\n" + a.t("msg.reg_fail") })
			return
		}
		if r.RepoID != 0 && res.Info.ID != 0 && res.Info.ID != r.RepoID {
			a.warn(a.t("msg.addr_other_repo", res.Info.FullName))
			return
		}
		if r.RepoID == 0 && !a.ask(a.t("msg.addr_unknown_id", r.Full(), res.Info.FullName)) {
			return
		}
		applyVerify(r, res)
		a.applyRename(r, res.Info.FullName)
	})
}
