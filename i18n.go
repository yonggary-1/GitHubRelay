package main

import "fmt"

// Languages supported by the UI.
const (
	LangKO = "ko"
	LangEN = "en"
)

// T returns the text for key in lang, formatted with args. Unknown keys return the key itself.
func T(lang, key string, args ...any) string {
	pair, ok := texts[key]
	if !ok {
		return key
	}
	s := pair[1]
	if lang == LangKO {
		s = pair[0]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Both returns the Korean and English text (used to size controls for the longer one).
func Both(key string) []string {
	p := texts[key]
	return []string{p[0], p[1]}
}

var texts = map[string][2]string{
	// ---- window ----
	"app.title":      {"GitHub Relay", "GitHub Relay"},
	"app.disclaimer": {"비공식 도구 · GitHub, Inc.와 무관하며 GitHub이 만들거나 보증하지 않습니다", "Unofficial tool · Not made, endorsed, or supported by GitHub, Inc."},
	"btn.about":      {"정보", "About"},
	"about.text": {
		"GitHub Relay %s\n프로젝트: %s\n\n채팅 세션이 만든 릴리즈 번들을 검수해 GitHub에 올리는 도구입니다. 토큰은 이 PC 밖으로 나가지 않습니다.\n\n비공식 도구입니다. GitHub Relay는 GitHub, Inc.가 만들거나 보증·지원하는 프로그램이 아닙니다. \"GitHub\"은 GitHub, Inc.의 상표입니다.\n\nMIT 라이선스\n데이터 파일: %s\n\nGitHub 프로젝트 페이지를 열까요?",
		"GitHub Relay %s\nProject: %s\n\nReviews release bundles made by chat sessions and publishes them to GitHub. Tokens never leave this PC.\n\nUnofficial tool. GitHub Relay is not made, endorsed, or supported by GitHub, Inc. \"GitHub\" is a trademark of GitHub, Inc.\n\nMIT License\nData file: %s\n\nOpen the project page on GitHub?",
	},
	"tab.repos":   {"리포지터리 등록", "Repositories"},
	"tab.release": {"새 버전 릴리즈", "New release"},
	"tab.history": {"기록", "History"},
	"tab.spec":    {"번들 규격 문서", "Bundle spec"},

	// ---- repositories page ----
	"col.repo":      {"리포지터리", "Repository"},
	"col.branch":    {"브랜치", "Branch"},
	"col.token":     {"토큰 상태", "Token"},
	"col.latest":    {"최신 버전", "Latest version"},
	"col.expires":   {"토큰 만료일", "Token expires"},
	"lbl.url":       {"리포지터리 주소", "Repository URL"},
	"lbl.token":     {"토큰", "Token"},
	"lbl.branch":    {"브랜치", "Branch"},
	"cue.url":       {"https://github.com/소유자/이름", "https://github.com/owner/name"},
	"cue.token":     {"github_pat_… (이 repo 전용 fine-grained 토큰)", "github_pat_… (fine-grained token for this repo only)"},
	"cue.branch":    {"비워두면 repo의 기본 브랜치", "Leave empty for the default branch"},
	"btn.register":  {"등록 및 검증", "Register && verify"},
	"btn.replace":   {"토큰 교체", "Replace token"},
	"btn.verify":    {"다시 검증", "Verify again"},
	"btn.remove":    {"등록 삭제", "Remove"},
	"btn.openrepo":  {"GitHub에서 열기", "Open on GitHub"},
	"btn.tokenpage": {"토큰 발급 페이지 열기", "Open token page"},
	"guide.token": {
		"토큰 발급 방법: GitHub → Settings → Developer settings → Fine-grained tokens → Generate new token.\r\n① Repository access: Only select repositories에서 이 repo 하나만 선택\r\n② Permissions: Contents를 Read and write로 설정 (그 외 권한은 필요 없음)\r\n③ Expiration: 만료일을 반드시 설정\r\n토큰은 이 PC의 Windows 계정으로 암호화되어 데이터 파일에 저장되며, 다른 PC로 옮기면 토큰만 다시 입력해야 합니다.",
		"How to create a token: GitHub → Settings → Developer settings → Fine-grained tokens → Generate new token.\r\n① Repository access: Only select repositories, and pick this one repository\r\n② Permissions: set Contents to Read and write (nothing else is needed)\r\n③ Expiration: always set an expiry date\r\nThe token is encrypted with this PC's Windows account. If you move the files to another PC, only the token must be entered again.",
	},
	"ts.ok":       {"정상", "OK"},
	"ts.expiring": {"만료 임박", "Expires soon"},
	"ts.expired":  {"만료됨", "Expired"},
	"ts.noaccess": {"접근 불가", "No access"},
	"ts.nowrite":  {"쓰기 권한 없음", "No write access"},
	"ts.needtok":  {"재입력 필요", "Re-enter token"},
	"ts.unknown":  {"확인 필요", "Not verified"},
	"none":        {"-", "-"},
	"default":     {"(기본)", "(default)"},

	"msg.url_invalid":     {"리포지터리 주소 형식이 올바르지 않습니다. 예: https://github.com/소유자/이름", "The repository address is not valid. Example: https://github.com/owner/name"},
	"msg.token_empty":     {"토큰을 입력하세요.", "Enter a token."},
	"msg.already":         {"이미 등록된 리포지터리입니다. 토큰을 바꾸려면 목록에서 선택한 뒤 \"토큰 교체\"를 누르세요.", "This repository is already registered. To change its token, select it and press \"Replace token\"."},
	"msg.select_repo":     {"먼저 목록에서 리포지터리를 선택하세요.", "Select a repository in the list first."},
	"msg.verifying":       {"검증 중…", "Verifying…"},
	"msg.reg_ok":          {"등록했습니다: %s", "Registered: %s"},
	"msg.reg_fail":        {"검증에 실패해 저장하지 않았습니다.", "Verification failed. Nothing was saved."},
	"msg.replace_ok":      {"토큰을 교체했습니다: %s", "Token replaced: %s"},
	"msg.verify_done":     {"검증 결과를 저장했습니다: %s", "Verification result saved: %s"},
	"msg.remove_q":        {"%s 등록을 삭제할까요?\n\n토큰이 데이터 파일에서 지워집니다.", "Remove %s?\n\nThe token will be erased from the data file."},
	"msg.remove_hist_q":   {"업로드 기록도 함께 지울까요?\n\n[예] 기록도 삭제   [아니요] 등록만 삭제 (기록은 다시 등록하면 보임)", "Erase the upload history too?\n\n[Yes] erase history   [No] remove the registration only (history returns if you register again)"},
	"msg.save_fail":       {"데이터 파일을 저장하지 못했습니다:\n%s\n\n프로그램을 쓰기 가능한 폴더(예: 문서, 바탕 화면)로 옮겨 주세요.", "Could not save the data file:\n%s\n\nMove the program to a writable folder (e.g. Documents or Desktop)."},
	"msg.load_fail":       {"데이터 파일을 읽지 못했습니다:\n%s\n\n파일이 손상되었을 수 있습니다. 프로그램을 종료합니다.", "Could not read the data file:\n%s\n\nIt may be damaged. The program will close."},
	"msg.need_token":      {"이 PC에서는 토큰을 풀 수 없습니다. \"토큰 교체\"로 토큰을 다시 입력하세요.", "The token cannot be decrypted on this PC. Use \"Replace token\" to enter it again."},
	"msg.busy":            {"다른 작업이 진행 중입니다. 끝난 뒤 다시 시도하세요.", "Another task is running. Try again when it finishes."},
	"msg.already_running": {"GitHub Relay가 이미 실행 중입니다.", "GitHub Relay is already running."},
	"msg.quit_busy":       {"업로드가 진행 중입니다. 지금 종료하면 업로드가 중간에 멈출 수 있습니다. 종료할까요?", "An upload is in progress. Closing now may leave it unfinished. Close anyway?"},
	"msg.held_history":    {"보관된 기록 %d건을 다시 연결했습니다.", "Reconnected %d archived history entries."},

	// ---- release page ----
	"lbl.repo":         {"리포지터리", "Repository"},
	"btn.browse":       {"찾아보기…", "Browse…"},
	"drop.hint":        {"번들 zip 파일을 이 창에 끌어다 놓거나 \"찾아보기…\"를 누르세요", "Drop a bundle zip onto this window, or press \"Browse…\""},
	"drop.need_repo":   {"먼저 리포지터리를 등록하세요 (리포지터리 등록 탭)", "Register a repository first (Repositories tab)"},
	"drop.file":        {"번들: %s", "Bundle: %s"},
	"col.result":       {"결과", "Result"},
	"col.check":        {"검수 항목", "Check"},
	"col.change":       {"변경", "Change"},
	"col.path":         {"파일", "File"},
	"lvl.pass":         {"통과", "Pass"},
	"lvl.warn":         {"경고", "Warning"},
	"lvl.fail":         {"실패", "Fail"},
	"ch.added":         {"추가", "Added"},
	"ch.modified":      {"수정", "Modified"},
	"ch.deleted":       {"삭제", "Deleted"},
	"chk.draft":        {"드래프트로 올리기 (웹에서 직접 공개)", "Upload as draft (publish it yourself on the web)"},
	"chk.approve_wf":   {"워크플로(.github/workflows) 변경을 확인했고 승인합니다", "I reviewed and approve the workflow (.github/workflows) changes"},
	"chk.approve_warn": {"위 경고 내용을 확인했습니다", "I have read the warnings above"},
	"btn.upload":       {"승인하고 업로드", "Approve && upload"},
	"btn.openrelease":  {"릴리즈 페이지 열기", "Open release page"},
	"btn.reanalyze":    {"다시 검수", "Check again"},
	"summary.none":     {"번들을 선택하면 검수 결과와 변경 내용이 여기에 표시됩니다.", "Select a bundle to see the review and the changes here."},
	"summary":          {"버전 %s (태그 %s) → %s 브랜치  ·  추가 %d  ·  수정 %d  ·  삭제 %d  ·  그대로 %d", "Version %s (tag %s) → branch %s  ·  added %d  ·  modified %d  ·  deleted %d  ·  unchanged %d"},
	"summary.local":    {"버전 %s (태그 %s)  ·  파일 %d개  ·  첨부 %d개", "Version %s (tag %s)  ·  %d files  ·  %d assets"},
	"st.analyzing":     {"검수 중… (GitHub과 비교)", "Checking… (comparing with GitHub)"},
	"st.ready":         {"검수 통과. 내용을 확인한 뒤 업로드하세요.", "Checks passed. Review the changes, then upload."},
	"st.blocked":       {"실패 항목이 있어 업로드할 수 없습니다.", "Upload is blocked because some checks failed."},
	"st.need_approval": {"경고를 확인하는 체크가 필요합니다.", "Tick the confirmation box(es) to continue."},
	"st.uploading":     {"업로드 중…", "Uploading…"},
	"st.done":          {"완료: %s", "Done: %s"},
	"st.done_draft":    {"드래프트로 올렸습니다. GitHub 웹에서 Publish를 눌러 공개하세요.", "Uploaded as a draft. Press Publish on the GitHub website to make it public."},
	"st.failed":        {"실패: %s", "Failed: %s"},
	"st.committed":     {"커밋은 올라갔지만 릴리즈가 완료되지 않았습니다: %s\n기록 탭에서 \"릴리즈만 다시 시도\"를 누르세요.", "The commit landed but the release is incomplete: %s\nUse \"Retry release only\" on the History tab."},
	"confirm.upload": {
		"%s 에 버전 %s 를 올립니다.\n\n브랜치: %s\n추가 %d · 수정 %d · 삭제 %d\n첨부 파일 %d개\n공개 방식: %s\n\n진행할까요?",
		"Publish version %s to %s?\n\nBranch: %s\nadded %d · modified %d · deleted %d\n%d assets\nVisibility: %s\n\nContinue?",
	},
	"vis.public":   {"바로 공개", "Published immediately"},
	"vis.draft":    {"드래프트", "Draft"},
	"filter.zip":   {"번들 zip (*.zip)", "Bundle zip (*.zip)"},
	"filter.md":    {"마크다운 (*.md)", "Markdown (*.md)"},
	"msg.only_zip": {"zip 파일 하나만 끌어다 놓으세요.", "Drop a single zip file."},

	// ---- upload progress ----
	"up.init":    {"빈 리포지터리에 첫 커밋 만드는 중", "Creating the first commit in the empty repository"},
	"up.blob":    {"파일 올리는 중: %s", "Uploading file: %s"},
	"up.tree":    {"파일 구조 만드는 중", "Building the file tree"},
	"up.commit":  {"커밋 만드는 중", "Creating the commit"},
	"up.ref":     {"%s 브랜치 갱신 중", "Updating branch %s"},
	"up.release": {"릴리즈 %s 만드는 중", "Creating release %s"},
	"up.asset":   {"첨부 파일 올리는 중: %s", "Uploading asset: %s"},
	"up.done":    {"완료", "Done"},

	// ---- history page ----
	"col.time":                 {"일시", "Time"},
	"col.version":              {"버전", "Version"},
	"col.status":               {"상태", "Status"},
	"col.changes":              {"변경 (추가/수정/삭제)", "Changes (add/mod/del)"},
	"col.note":                 {"비고", "Note"},
	"btn.sync":                 {"GitHub와 동기화", "Sync with GitHub"},
	"btn.opencommit":           {"커밋 보기", "View commit"},
	"btn.retry":                {"릴리즈만 다시 시도", "Retry release only"},
	"stt.success":              {"공개됨", "Published"},
	"stt.draft":                {"드래프트", "Draft"},
	"stt.failed":               {"실패", "Failed"},
	"stt.committed":            {"커밋됨, 릴리즈 미완료", "Committed, release incomplete"},
	"stt.deleted":              {"GitHub에서 삭제됨", "Deleted on GitHub"},
	"stt.external":             {"GitHub에서 가져옴", "Found on GitHub"},
	"msg.synced":               {"동기화했습니다: 릴리즈 %d개 확인", "Synced: %d releases checked"},
	"msg.select_entry":         {"목록에서 기록을 선택하세요.", "Select an entry in the list."},
	"msg.no_retry":             {"이 기록은 다시 시도할 필요가 없습니다.", "This entry does not need a retry."},
	"msg.retry_bundle_missing": {"원래 번들 파일을 찾을 수 없습니다:\n%s\n\n같은 번들을 다시 선택하세요.", "The original bundle file was not found:\n%s\n\nSelect the same bundle again."},
	"msg.retry_q":              {"버전 %s 의 릴리즈를 다시 시도할까요?\n(커밋은 이미 올라가 있으며 다시 올리지 않습니다)", "Retry the release for version %s?\n(The commit is already on GitHub and will not be uploaded again.)"},
	"hist.empty":               {"기록이 없습니다", "No history"},

	// ---- spec page ----
	"lbl.spec_repo":     {"대상 리포지터리", "Target repository"},
	"spec.none":         {"(지정 안 함)", "(none)"},
	"btn.copy":          {"클립보드로 복사", "Copy to clipboard"},
	"btn.save_md":       {".md 파일로 저장…", "Save as .md…"},
	"msg.spec_no_repo":  {"대상 리포지터리를 고르지 않았습니다.\n\n이대로 내보내면 문서에 리포 주소 대신 OWNER/REPO 자리표시자가 들어가고, 채팅 세션에는 주소를 사용자에게 물어보라고 안내됩니다.\n\n그대로 진행할까요?", "No target repository is selected.\n\nThe spec will contain the placeholder OWNER/REPO instead of an address, and tells the chat session to ask you for it.\n\nContinue anyway?"},
	"msg.expiry_notice": {"토큰 만료를 확인하세요:\n\n%s\n\n리포지터리 등록 탭에서 \"토큰 교체\"로 새 토큰을 넣을 수 있습니다.", "Check these tokens:\n\n%s\n\nUse \"Replace token\" on the Repositories tab to enter a new one."},
	"msg.copied":        {"규격 문서를 클립보드에 복사했습니다. 채팅 세션에 붙여넣으세요.", "The spec was copied. Paste it into the chat session."},
	"msg.saved":         {"저장했습니다: %s", "Saved: %s"},

	// ---- bundle checks ----
	"chk.zip_open":          {"zip 파일을 열 수 없습니다: %v", "Cannot open the zip file: %v"},
	"chk.too_many_files":    {"파일이 너무 많습니다 (최대 %v개)", "Too many files (max %v)"},
	"chk.too_large":         {"번들 전체 크기가 1GB를 넘습니다", "The bundle is larger than 1 GB in total"},
	"chk.zip_read":          {"zip 안의 파일을 읽을 수 없습니다: %v (%v)", "Cannot read a file in the zip: %v (%v)"},
	"chk.duplicate_entry":   {"zip 안에 같은 경로가 두 번 있습니다: %v", "The zip contains the same path twice: %v"},
	"chk.unsafe_paths":      {"위험한 경로가 있습니다 (폴더 밖, 절대 경로 등): %v", "Unsafe paths (outside the folder, absolute, etc.): %v"},
	"chk.paths_ok":          {"zip 경로 안전", "Zip paths are safe"},
	"chk.unwrapped":         {"최상위 폴더 \"%v\" 안의 내용을 번들로 사용", "Using the contents of top-level folder \"%v\""},
	"chk.missing":           {"필수 파일 없음: %v", "Required file missing: %v"},
	"chk.manifest_json":     {"release.json 형식 오류: %v", "release.json is invalid: %v"},
	"chk.spec_version":      {"spec_version %v 은(는) 지원하지 않습니다 (지원: %v)", "spec_version %v is not supported (supported: %v)"},
	"chk.field_missing":     {"release.json에 필수 항목 없음: %v", "release.json is missing: %v"},
	"chk.repo_invalid":      {"release.json의 repo 형식 오류: %v", "release.json repo is invalid: %v"},
	"chk.repo_mismatch":     {"번들의 대상 repo(%v)가 선택한 repo(%v)와 다릅니다", "The bundle targets %v, but %v is selected"},
	"chk.version_format":    {"버전 형식 오류: %v (예: 0.1 또는 0.1.1)", "Invalid version: %v (e.g. 0.1 or 0.1.1)"},
	"chk.tag_format":        {"태그 형식 오류: %v", "Invalid tag: %v"},
	"chk.language_invalid":  {"languages 항목 오류: %v (예: en, ko, 중복 불가)", "Invalid languages entry: %v (e.g. en, ko; no duplicates)"},
	"chk.manifest_ok":       {"release.json 정상 (태그 %v, 언어 %v)", "release.json OK (tag %v, languages %v)"},
	"chk.empty_doc":         {"문서가 비어 있습니다: %v", "Document is empty: %v"},
	"chk.docs_ok":           {"README와 릴리즈 노트 확인 (%v개 언어)", "README and release notes found (%v languages)"},
	"chk.doc_in_src":        {"%v 은(는) src/ 밖(번들 최상위)에 두어야 합니다", "%v must be at the bundle root, not inside src/"},
	"chk.git_dir":           {"src/ 안에 .git 폴더가 있습니다", "src/ contains a .git folder"},
	"chk.src_empty":         {"src/ 폴더가 없거나 비어 있습니다", "src/ is missing or empty"},
	"chk.src_ok":            {"소스 파일 %v개", "%v source files"},
	"chk.ignored":           {"규격에 없는 파일은 무시됩니다: %v", "Files outside the spec are ignored: %v"},
	"chk.case_collision":    {"대소문자만 다른 파일이 있습니다: %v / %v", "Files differ only by letter case: %v / %v"},
	"chk.file_too_big":      {"파일이 100MB를 넘어 GitHub에 올릴 수 없습니다: %v (%v)", "File over 100 MB cannot be stored on GitHub: %v (%v)"},
	"chk.file_big":          {"큰 파일 (50MB 초과): %v (%v)", "Large file (over 50 MB): %v (%v)"},
	"chk.sizes_ok":          {"파일 크기 정상", "File sizes OK"},
	"chk.asset_not_dist":    {"첨부 파일은 dist/ 안에 있어야 합니다: %v", "Assets must be inside dist/: %v"},
	"chk.asset_missing":     {"첨부 파일이 없습니다: %v", "Asset not found: %v"},
	"chk.asset_dup":         {"같은 이름의 첨부 파일이 둘 이상: %v", "More than one asset named %v"},
	"chk.asset_empty":       {"첨부 파일이 비어 있습니다: %v", "Asset is empty: %v"},
	"chk.asset_too_big":     {"첨부 파일이 2GB를 넘습니다: %v", "Asset is over 2 GB: %v"},
	"chk.assets_ok":         {"첨부 파일 %v개 확인", "%v assets found"},
	"chk.secret_file":       {"비밀 정보 파일로 보입니다: %v", "Looks like a secret file: %v"},
	"chk.secret_found":      {"비밀 정보가 들어 있습니다: %v (%v)", "Contains a secret: %v (%v)"},
	"chk.secret_binary":     {"바이너리 파일에 비밀 정보처럼 보이는 부분이 있습니다 (오탐일 수 있음): %v (%v)", "A binary file contains something that looks like a secret (may be a false positive): %v (%v)"},
	"chk.secrets_ok":        {"토큰·키 등 비밀 정보 없음", "No tokens, keys or other secrets"},
	"chk.archived":          {"보관(archived)된 리포지터리라 올릴 수 없습니다", "The repository is archived and read-only"},
	"chk.empty_branch":      {"빈 리포지터리의 첫 업로드는 기본 브랜치(%[2]v)로만 가능합니다. 등록된 브랜치: %[1]v", "The first upload to an empty repository must go to its default branch (%[2]v). Registered branch: %[1]v"},
	"chk.empty_repo":        {"빈 리포지터리: 첫 커밋을 만든 뒤 %v 브랜치에 올립니다", "Empty repository: a first commit will be created on %v"},
	"chk.branch_missing":    {"브랜치가 없습니다: %v", "Branch not found: %v"},
	"chk.tree_truncated":    {"리포지터리가 너무 커서 전체 파일 목록을 받을 수 없습니다", "The repository is too large to list all files"},
	"chk.branch_ok":         {"%v 브랜치의 현재 상태와 비교 완료", "Compared with the current state of %v"},
	"chk.tag_exists":        {"이미 있는 태그(또는 릴리즈)입니다: %v", "Tag or release already exists: %v"},
	"chk.version_not_newer": {"버전 %v 은(는) 직전 버전 %v 보다 높아야 합니다", "Version %v must be higher than the previous version %v"},
	"chk.version_ok":        {"버전 %v (직전 %v)", "Version %v (previous %v)"},
	"chk.version_first":     {"첫 릴리즈: 버전 %v", "First release: version %v"},
	"chk.no_changes":        {"리포지터리와 비교해 바뀐 파일이 없습니다", "No files differ from the repository"},
	"chk.workflow":          {"GitHub Actions 워크플로 추가/변경: %v", "GitHub Actions workflow added/changed: %v"},
	"chk.many_deletes":      {"파일을 많이 삭제합니다: %v / %v개", "Many files will be deleted: %v of %v"},

	// ---- token verification ----
	"reg.access_ok":           {"리포지터리 접근 가능: %v", "Repository is accessible: %v"},
	"reg.expired":             {"토큰이 만료되었습니다 (%v)", "The token has expired (%v)"},
	"reg.expiring":            {"토큰이 곧 만료됩니다 (%v)", "The token expires soon (%v)"},
	"reg.expires":             {"토큰 만료일: %v", "Token expires: %v"},
	"reg.no_expiry":           {"토큰 만료일이 없습니다. 만료일을 설정한 토큰을 권장합니다", "The token has no expiry date. A token with an expiry is recommended"},
	"reg.empty_write_unknown": {"빈 리포지터리라 쓰기 권한은 첫 업로드 때 확인됩니다", "The repository is empty, so write access will be confirmed on the first upload"},
	"reg.no_write":            {"쓰기 권한이 없습니다. 토큰의 Contents 권한을 Read and write로 설정하세요", "No write access. Set the token's Contents permission to Read and write"},
	"reg.write_ok":            {"쓰기 권한 확인", "Write access confirmed"},

	// ---- errors ----
	"err.401":            {"토큰이 올바르지 않거나 만료되었습니다", "The token is invalid or expired"},
	"err.403":            {"권한이 없거나 요청이 제한되었습니다: %v", "Forbidden or rate limited: %v"},
	"err.404":            {"리포지터리를 찾을 수 없거나 이 토큰으로 접근할 수 없습니다", "Repository not found, or this token cannot access it"},
	"err.409":            {"리포지터리 상태 충돌: %v", "Repository conflict: %v"},
	"err.422":            {"GitHub이 요청을 거부했습니다: %v", "GitHub rejected the request: %v"},
	"err.api":            {"GitHub 오류: %v", "GitHub error: %v"},
	"err.network":        {"네트워크 오류: %v", "Network error: %v"},
	"err.moved":          {"검수 이후 브랜치가 바뀌었습니다. 다시 검수하세요 (리포지터리는 바뀌지 않았습니다)", "The branch changed after the check. Check again (nothing was changed)"},
	"err.bundle_changed": {"번들이 원래 업로드한 것과 다르거나 검수를 통과하지 못합니다", "The bundle differs from the original or no longer passes the checks"},
}
