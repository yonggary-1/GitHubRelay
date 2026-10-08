//go:build windows

package main

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	crypt32  = syscall.NewLazyDLL("crypt32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW       = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pSendMessageW           = user32.NewProc("SendMessageW")
	pShowWindow             = user32.NewProc("ShowWindow")
	pUpdateWindow           = user32.NewProc("UpdateWindow")
	pSetWindowPos           = user32.NewProc("SetWindowPos")
	pSetWindowTextW         = user32.NewProc("SetWindowTextW")
	pGetWindowTextW         = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	pEnableWindow           = user32.NewProc("EnableWindow")
	pMessageBoxW            = user32.NewProc("MessageBoxW")
	pLoadCursorW            = user32.NewProc("LoadCursorW")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pGetClientRect          = user32.NewProc("GetClientRect")
	pGetDpiForWindow        = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
	pInvalidateRect         = user32.NewProc("InvalidateRect")
	pRedrawWindow           = user32.NewProc("RedrawWindow")
	pGetWindowLongPtrW      = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW      = user32.NewProc("SetWindowLongPtrW")
	pGetDC                  = user32.NewProc("GetDC")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pDrawTextW              = user32.NewProc("DrawTextW")
	pOpenClipboard          = user32.NewProc("OpenClipboard")
	pEmptyClipboard         = user32.NewProc("EmptyClipboard")
	pSetClipboardData       = user32.NewProc("SetClipboardData")
	pCloseClipboard         = user32.NewProc("CloseClipboard")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pGetSysColorBrush       = user32.NewProc("GetSysColorBrush")
	pGetSysColor            = user32.NewProc("GetSysColor")
	pSetFocus               = user32.NewProc("SetFocus")
	pGetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")

	pCreateFontW           = gdi32.NewProc("CreateFontW")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pSelectObject          = gdi32.NewProc("SelectObject")
	pGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	pSetBkMode             = gdi32.NewProc("SetBkMode")
	pSetTextColor          = gdi32.NewProc("SetTextColor")

	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	pGlobalLock               = kernel32.NewProc("GlobalLock")
	pGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	pGlobalFree               = kernel32.NewProc("GlobalFree")
	pCreateMutexW             = kernel32.NewProc("CreateMutexW")
	pGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
	pLocalFree                = kernel32.NewProc("LocalFree")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")

	pDragAcceptFiles = shell32.NewProc("DragAcceptFiles")
	pDragQueryFileW  = shell32.NewProc("DragQueryFileW")
	pDragFinish      = shell32.NewProc("DragFinish")
	pShellExecuteW   = shell32.NewProc("ShellExecuteW")

	pGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")

	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_TABSTOP          = 0x00010000
	WS_GROUP            = 0x00020000
	WS_VSCROLL          = 0x00200000
	WS_HSCROLL          = 0x00100000
	WS_BORDER           = 0x00800000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000

	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_ACCEPTFILES   = 0x00000010
	WS_EX_CONTROLPARENT = 0x00010000

	BS_PUSHBUTTON      = 0
	BS_AUTOCHECKBOX    = 3
	BS_AUTORADIOBUTTON = 9
	BS_MULTILINE       = 0x2000

	ES_MULTILINE   = 0x4
	ES_PASSWORD    = 0x20
	ES_AUTOVSCROLL = 0x40
	ES_AUTOHSCROLL = 0x80
	ES_READONLY    = 0x800

	SS_LEFT         = 0
	SS_CENTER       = 1
	SS_NOPREFIX     = 0x80
	SS_CENTERIMAGE  = 0x200
	SS_ENDELLIPSIS  = 0x4000
	SS_PATHELLIPSIS = 0x8000

	CBS_DROPDOWNLIST = 3
	CBS_HASSTRINGS   = 0x200

	CB_ADDSTRING    = 0x143
	CB_GETCURSEL    = 0x147
	CB_RESETCONTENT = 0x14B
	CB_SETCURSEL    = 0x14E
	CBN_SELCHANGE   = 1

	BM_GETCHECK = 0xF0
	BM_SETCHECK = 0xF1
	BN_CLICKED  = 0

	EM_SETLIMITTEXT = 0xC5
	EM_SETCUEBANNER = 0x1501

	LVS_REPORT        = 0x1
	LVS_SINGLESEL     = 0x4
	LVS_SHOWSELALWAYS = 0x8
	LVS_NOSORTHEADER  = 0x8000

	LVM_DELETEALLITEMS           = 0x1009
	LVM_GETNEXTITEM              = 0x100C
	LVM_ENSUREVISIBLE            = 0x1013
	LVM_SETCOLUMNWIDTH           = 0x101E
	LVM_SETITEMSTATE             = 0x102B
	LVM_SETEXTENDEDLISTVIEWSTYLE = 0x1036
	LVM_INSERTITEMW              = 0x104D
	LVM_SETCOLUMNW               = 0x1060
	LVM_INSERTCOLUMNW            = 0x1061
	LVM_SETITEMTEXTW             = 0x1074

	LVS_EX_GRIDLINES     = 0x1
	LVS_EX_FULLROWSELECT = 0x20
	LVS_EX_LABELTIP      = 0x4000
	LVS_EX_DOUBLEBUFFER  = 0x10000

	LVCF_FMT      = 0x1
	LVCF_WIDTH    = 0x2
	LVCF_TEXT     = 0x4
	LVCF_SUBITEM  = 0x8
	LVIF_TEXT     = 0x1
	LVIF_STATE    = 0x8
	LVNI_SELECTED = 0x2
	LVIS_FOCUSED  = 0x1
	LVIS_SELECTED = 0x2

	LVN_ITEMCHANGED = -101
	NM_DBLCLK       = -3

	TCM_GETCURSEL   = 0x130B
	TCM_SETCURSEL   = 0x130C
	TCM_ADJUSTRECT  = 0x1328
	TCM_SETITEMW    = 0x133D
	TCM_INSERTITEMW = 0x133E
	TCIF_TEXT       = 0x1
	TCN_SELCHANGE   = -551

	PBM_SETPOS     = 0x402
	PBM_SETRANGE32 = 0x406

	WM_DESTROY        = 0x2
	WM_SIZE           = 0x5
	WM_CLOSE          = 0x10
	WM_GETMINMAXINFO  = 0x24
	WM_SETFONT        = 0x30
	WM_NOTIFY         = 0x4E
	WM_COMMAND        = 0x111
	WM_CTLCOLORSTATIC = 0x138
	WM_DROPFILES      = 0x233
	WM_DPICHANGED     = 0x2E0
	WM_APP            = 0x8000

	SW_HIDE = 0
	SW_SHOW = 5

	MB_OK              = 0x0
	MB_YESNOCANCEL     = 0x3
	MB_YESNO           = 0x4
	MB_ICONERROR       = 0x10
	MB_ICONQUESTION    = 0x20
	MB_ICONWARNING     = 0x30
	MB_ICONINFORMATION = 0x40
	IDYES              = 6
	IDNO               = 7

	COLOR_WINDOW   = 5
	COLOR_GRAYTEXT = 17
	IDC_ARROW      = 32512

	CW_USEDEFAULT = ^uintptr(0x7FFFFFFF) // 0x80000000 as int32

	SWP_NOZORDER   = 0x4
	SWP_NOACTIVATE = 0x10

	OFN_OVERWRITEPROMPT = 0x2
	OFN_NOCHANGEDIR     = 0x8
	OFN_PATHMUSTEXIST   = 0x800
	OFN_FILEMUSTEXIST   = 0x1000
	OFN_EXPLORER        = 0x80000

	GMEM_MOVEABLE  = 0x2
	CF_UNICODETEXT = 13

	TRANSPARENT = 1

	DT_WORDBREAK   = 0x10
	DT_CALCRECT    = 0x400
	DT_NOPREFIX    = 0x800
	DT_EDITCONTROL = 0x2000

	FW_NORMAL         = 400
	FW_BOLD           = 700
	DEFAULT_CHARSET   = 1
	CLEARTYPE_QUALITY = 5

	SM_CYVSCROLL = 20
)

type POINT struct{ X, Y int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type SIZE struct{ CX, CY int32 }

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

type NMHDR struct {
	HwndFrom uintptr
	IdFrom   uintptr
	Code     int32
}

type NMLISTVIEW struct {
	Hdr       NMHDR
	IItem     int32
	ISubItem  int32
	UNewState uint32
	UOldState uint32
	UChanged  uint32
	PtAction  POINT
	LParam    uintptr
}

type LVCOLUMNW struct {
	Mask       uint32
	Fmt        int32
	Cx         int32
	PszText    *uint16
	CchTextMax int32
	ISubItem   int32
	IImage     int32
	IOrder     int32
	CxMin      int32
	CxDefault  int32
	CxIdeal    int32
}

type LVITEMW struct {
	Mask       uint32
	IItem      int32
	ISubItem   int32
	State      uint32
	StateMask  uint32
	PszText    *uint16
	CchTextMax int32
	IImage     int32
	LParam     uintptr
	IIndent    int32
	IGroupId   int32
	CColumns   uint32
	PuColumns  uintptr
	PiColFmt   uintptr
	IGroup     int32
}

type TCITEMW struct {
	Mask        uint32
	DwState     uint32
	DwStateMask uint32
	PszText     *uint16
	CchTextMax  int32
	IImage      int32
	LParam      uintptr
}

type INITCOMMONCONTROLSEX struct {
	DwSize uint32
	DwICC  uint32
}

type OPENFILENAMEW struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}

// u16 converts a Go string to a NUL-terminated UTF-16 pointer (embedded NULs are dropped).
func u16(s string) *uint16 {
	r := []rune{}
	for _, c := range s {
		if c != 0 {
			r = append(r, c)
		}
	}
	a := utf16.Encode(r)
	a = append(a, 0)
	return &a[0]
}

func send(h uintptr, msg uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(h, uintptr(msg), w, l)
	return r
}

func setText(h uintptr, s string) { pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(u16(s)))) }

func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(h)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func enable(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	pEnableWindow.Call(h, v)
}

func show(h uintptr, on bool) {
	if on {
		pShowWindow.Call(h, SW_SHOW)
	} else {
		pShowWindow.Call(h, SW_HIDE)
	}
}

func move(h uintptr, x, y, w, hgt int) {
	if w < 0 {
		w = 0
	}
	if hgt < 0 {
		hgt = 0
	}
	pSetWindowPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), SWP_NOZORDER|SWP_NOACTIVATE)
}

func checked(h uintptr) bool { return send(h, BM_GETCHECK, 0, 0) == 1 }

func setChecked(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	send(h, BM_SETCHECK, v, 0)
}

func msgBox(owner uintptr, text, title string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
	return int(r)
}

func openURL(u string) {
	if u == "" {
		return
	}
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(u))), 0, 0, SW_SHOW)
}

func loword(x uintptr) uint32 { return uint32(x) & 0xFFFF }
func hiword(x uintptr) uint32 { return (uint32(x) >> 16) & 0xFFFF }

// setClipboard puts Unicode text on the clipboard.
func setClipboard(owner uintptr, s string) bool {
	a := utf16.Encode([]rune(s))
	a = append(a, 0)
	if r, _, _ := pOpenClipboard.Call(owner); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	size := uintptr(len(a) * 2)
	hmem, _, _ := pGlobalAlloc.Call(GMEM_MOVEABLE, size)
	if hmem == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(hmem)
	if p == 0 {
		pGlobalFree.Call(hmem)
		return false
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(a))
	copy(dst, a)
	pGlobalUnlock.Call(hmem)
	if r, _, _ := pSetClipboardData.Call(CF_UNICODETEXT, hmem); r == 0 {
		pGlobalFree.Call(hmem)
		return false
	}
	return true
}

// fileDialog shows the Open or Save dialog and returns the chosen path.
func fileDialog(owner uintptr, save bool, filterName, pattern, defName, defExt string) (string, bool) {
	filter := utf16.Encode([]rune(filterName + "\x00" + pattern + "\x00\x00"))
	buf := make([]uint16, 32768)
	copy(buf, utf16.Encode([]rune(defName)))
	ofn := OPENFILENAMEW{
		HwndOwner:   owner,
		LpstrFilter: &filter[0],
		LpstrFile:   &buf[0],
		NMaxFile:    uint32(len(buf)),
		Flags:       OFN_EXPLORER | OFN_NOCHANGEDIR | OFN_PATHMUSTEXIST,
	}
	ofn.LStructSize = uint32(unsafe.Sizeof(ofn))
	if defExt != "" {
		ofn.LpstrDefExt = u16(defExt)
	}
	var r uintptr
	if save {
		ofn.Flags |= OFN_OVERWRITEPROMPT
		r, _, _ = pGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	} else {
		ofn.Flags |= OFN_FILEMUSTEXIST
		r, _, _ = pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	}
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

// redrawAll repaints a window and all of its children, including their borders.
func redrawAll(h uintptr) {
	const rdwInvalidate, rdwErase, rdwAllChildren, rdwFrame = 0x1, 0x4, 0x80, 0x400
	pRedrawWindow.Call(h, 0, 0, rdwInvalidate|rdwErase|rdwAllChildren|rdwFrame)
}

// openFilesDialog lets the user pick one or more files; paths are returned in full.
func openFilesDialog(owner uintptr, filterName, pattern string) []string {
	const ofnAllowMultiSelect = 0x200
	filter := utf16.Encode([]rune(filterName + "\x00" + pattern + "\x00\x00"))
	buf := make([]uint16, 1<<16)
	ofn := OPENFILENAMEW{
		HwndOwner:   owner,
		LpstrFilter: &filter[0],
		LpstrFile:   &buf[0],
		NMaxFile:    uint32(len(buf)),
		Flags:       OFN_EXPLORER | OFN_NOCHANGEDIR | OFN_PATHMUSTEXIST | OFN_FILEMUSTEXIST | ofnAllowMultiSelect,
	}
	ofn.LStructSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return nil
	}
	// Explorer style: "dir\0name1\0name2\0\0", or a single full path "path\0\0".
	var parts []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			parts = append(parts, string(utf16.Decode(buf[start:i])))
			start = i + 1
		}
	}
	if len(parts) <= 1 {
		return parts
	}
	dir := parts[0]
	out := make([]string, 0, len(parts)-1)
	for _, n := range parts[1:] {
		out = append(out, dir+`\`+n)
	}
	return out
}
