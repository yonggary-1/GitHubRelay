package main

import (
	"fmt"
	"strings"
)

// SpecTarget fills the spec with a concrete repository when one is selected.
type SpecTarget struct {
	Repo     string // owner/name
	Previous string // last released version, "" if none
	Next     string // suggested next version
}

// SpecFor builds the target info for a registered repository.
func SpecFor(r *RepoEntry) *SpecTarget {
	if r == nil {
		return nil
	}
	t := &SpecTarget{Repo: r.Full(), Next: "0.1"}
	if h := r.LatestSuccess(); h != nil {
		if v, err := ParseVersion(h.Version); err == nil {
			t.Previous = v.String()
			t.Next = NextVersion(v).String()
		}
	}
	return t
}

// SpecMarkdown returns the bundle specification for chat sessions, in lang.
func SpecMarkdown(lang string, t *SpecTarget) string {
	repo, ver, prev := "OWNER/REPO", "0.1", ""
	if t != nil {
		repo, ver, prev = t.Repo, t.Next, t.Previous
	}
	name := repo[strings.Index(repo, "/")+1:]
	example := fmt.Sprintf(`{
  "spec_version": %d,
  "repo": "%s",
  "version": "%s",
  "tag": "v%s",
  "title": "v%s",
  "commit_message": "Release v%s",
  "languages": ["en", "ko"],
  "assets": ["dist/%s.exe"]
}`, SpecVersion, repo, ver, ver, ver, ver, name)
	if lang == LangKO {
		return specKO(repo, ver, prev, name, example, t == nil)
	}
	return specEN(repo, ver, prev, name, example, t == nil)
}

func specKO(repo, ver, prev, name, example string, unknown bool) string {
	var b strings.Builder
	w := func(s string, a ...any) { fmt.Fprintf(&b, s, a...) }
	w("# GitHub Relay 번들 규격 v%d\n\n", SpecVersion)
	w("이 문서는 개발 결과물을 GitHub에 올리기 위한 **릴리즈 번들(zip)** 형식이다. ")
	w("너(채팅 세션)는 GitHub에 직접 접속하거나 토큰을 다루지 않는다. 이 규격대로 zip 파일 하나만 만들어 사용자에게 전달하면, 사용자의 PC에서 GitHub Relay가 검수한 뒤 업로드한다. 규격을 하나라도 어기면 업로드가 거부된다.\n\n")
	w("## 이번 릴리즈 정보\n\n")
	if unknown {
		w("- 대상 리포지터리: **지정되지 않음**. `release.json`의 `repo` 값을 **추측하지 말고**, 작업을 시작하기 전에 사용자에게 GitHub 리포지터리 주소(`소유자/이름`)를 물어볼 것. 이 문서의 `OWNER/REPO`는 자리표시자다.\n")
		w("- 직전 버전도 알 수 없다. 첫 릴리즈가 아니라면 사용자에게 직전 버전을 확인할 것.\n")
		w("- 결과물 파일 이름 권장: `<리포 이름>-v<버전>-bundle.zip`\n\n")
	} else {
		w("- 대상 리포지터리: `%s`\n", repo)
		if prev != "" {
			w("- 직전 버전: `%s` → 이번 버전은 이보다 높아야 함 (권장: `%s`)\n", prev, ver)
		} else {
			w("- 첫 릴리즈 (권장 버전: `%s`)\n", ver)
		}
		w("- 결과물 파일 이름 권장: `%s-v%s-bundle.zip`\n\n", name, ver)
	}
	w("## 번들 구조\n\n```\n")
	w("%s-v%s-bundle.zip\n", name, ver)
	w("├─ release.json             필수. 릴리즈 정보\n")
	w("├─ README.md                필수. languages 첫 번째 언어\n")
	w("├─ README.<코드>.md          languages 나머지 언어마다 필수 (예: README.ko.md)\n")
	w("├─ RELEASE_NOTES.md         필수. languages 첫 번째 언어\n")
	w("├─ RELEASE_NOTES.<코드>.md   languages 나머지 언어마다 필수\n")
	w("├─ src/                     필수. 리포지터리에 커밋될 전체 소스\n")
	w("└─ dist/                    선택. 릴리즈에 첨부할 빌드 결과물\n```\n\n")
	w("- zip 최상위에 위 항목이 바로 보여야 한다. (폴더 하나로 감싼 zip도 허용)\n")
	w("- README와 RELEASE_NOTES 파일은 업로드 시 리포지터리 **루트**에 커밋된다. 같은 이름의 파일을 `src/` 안에 두면 안 된다.\n\n")
	w("## release.json\n\n```json\n%s\n```\n\n", example)
	w("| 항목 | 필수 | 설명 |\n| --- | --- | --- |\n")
	w("| spec_version | 필수 | 항상 `%d` |\n", SpecVersion)
	w("| repo | 필수 | `소유자/이름`. 반드시 `%s` |\n", repo)
	w("| version | 필수 | 숫자 두 자리(`0.1`) 또는 세 자리(`0.1.1`). 앞에 v를 붙이지 않는다 |\n")
	w("| tag | 선택 | 생략하면 `v` + version |\n")
	w("| title | 선택 | 릴리즈 제목. 생략하면 태그 |\n")
	w("| commit_message | 선택 | 생략하면 `Release <태그>` |\n")
	w("| languages | 필수 | 문서 언어 코드 목록. 첫 번째가 기본 언어 (예: `[\"en\", \"ko\"]`) |\n")
	w("| assets | 선택 | 릴리즈에 첨부할 `dist/` 안의 파일 경로 목록. 없으면 `[]` |\n\n")
	w("release.json에는 위 항목 외의 키를 넣지 않는다.\n\n")
	w("## 버전 규칙\n\n")
	w("- 형식: `주.부` 또는 `주.부.수정` (예: `0.1`, `0.2`, `1.0.3`). 숫자만 쓴다.\n")
	w("- 직전 버전보다 높아야 한다. 숫자로 비교한다 (`0.10` > `0.9`, `0.1` = `0.1.0`).\n")
	w("- 이미 있는 태그·릴리즈와 같으면 거부된다.\n\n")
	w("## 언어 규칙\n\n")
	w("- `languages`에 적은 모든 언어로 README와 릴리즈 노트를 작성한다.\n")
	w("- 첫 번째 언어는 `README.md` / `RELEASE_NOTES.md`, 나머지는 `README.<코드>.md` / `RELEASE_NOTES.<코드>.md`.\n")
	w("- 각 README 맨 위에 다른 언어 README로 가는 링크를 둔다. 예: `English | [한국어](README.ko.md)`\n")
	w("- 릴리즈 노트는 언어별 파일이 순서대로 합쳐져 릴리즈 본문 하나가 된다.\n\n")
	w("## README 규칙\n\n")
	w("1. 맨 위: 언어 전환 링크\n")
	w("2. 첫 단락: 이 프로그램을 **만든 목적** (무엇을, 왜)\n")
	w("3. 그 다음: 실행 환경(지원 OS·하드웨어), 설치·사용 방법, 라이선스\n\n")
	w("## src/ 규칙\n\n")
	w("- `src/`는 리포지터리의 **전체 스냅샷**이다. `src/`에 없는 파일은 리포지터리에서 **삭제**된다. 부분만 넣지 않는다.\n")
	w("- 빌드 산출물, 캐시, 임시 파일은 넣지 않는다 (`.gitignore`로 관리할 대상).\n")
	w("- `.git` 폴더, 심볼릭 링크를 넣지 않는다.\n")
	w("- 파일 하나는 100MB 미만 (50MB 초과는 경고).\n")
	w("- 대소문자만 다른 파일 이름을 만들지 않는다.\n\n")
	w("## dist/ 규칙\n\n")
	w("- 사용자가 내려받을 실행 파일·패키지만 넣고, `release.json`의 `assets`에 경로를 적는다.\n")
	w("- 첨부 파일 이름은 서로 달라야 한다. 이름에 버전을 넣지 않아도 된다.\n\n")
	w("## 금지 사항 (발견 시 업로드 거부)\n\n")
	w("- 토큰, API 키, 비밀번호, 개인키 등 비밀 정보 (`ghp_`, `github_pat_`, `sk-`, `-----BEGIN … PRIVATE KEY-----` 등)\n")
	w("- `.env`, `*.pem`, `*.key`, `*.pfx`, `id_rsa` 같은 비밀 파일 (`.env.example`은 허용)\n")
	w("- `../`나 절대 경로가 들어간 zip 경로\n")
	w("- 사용자가 요청하지 않은 GitHub Actions 워크플로(`.github/workflows/`) 추가·변경 (추가하면 사용자가 별도로 승인해야 함)\n\n")
	w("## 제출 전 체크리스트\n\n")
	if unknown {
		w("- [ ] release.json의 repo를 사용자에게 확인받은 주소로 적었다 (OWNER/REPO 그대로 두지 않음)\n")
	} else {
		w("- [ ] release.json의 repo가 `%s`이고 version이 직전 버전보다 높다\n", repo)
	}
	w("- [ ] languages의 모든 언어로 README와 RELEASE_NOTES를 작성했다\n")
	w("- [ ] README 첫 단락에 만든 목적이 있다\n")
	w("- [ ] src/에 리포지터리 전체 소스가 들어 있다\n")
	w("- [ ] assets에 적은 파일이 dist/에 모두 있다\n")
	w("- [ ] 비밀 정보와 비밀 파일이 없다\n")
	w("- [ ] zip 파일 하나로 사용자에게 전달했다\n")
	return b.String()
}

func specEN(repo, ver, prev, name, example string, unknown bool) string {
	var b strings.Builder
	w := func(s string, a ...any) { fmt.Fprintf(&b, s, a...) }
	w("# GitHub Relay Bundle Spec v%d\n\n", SpecVersion)
	w("This document defines the **release bundle (zip)** used to publish your work to GitHub. ")
	w("You (the chat session) never connect to GitHub or handle tokens. Produce exactly one zip file that follows this spec and hand it to the user; GitHub Relay on the user's PC reviews it and uploads it. Any violation makes the upload fail.\n\n")
	w("## This release\n\n")
	if unknown {
		w("- Target repository: **not specified**. **Do not guess** the `repo` value in `release.json`; ask the user for the GitHub repository (`owner/name`) before you start. `OWNER/REPO` in this document is a placeholder.\n")
		w("- The previous version is unknown too. Unless this is the first release, ask the user for it.\n")
		w("- Suggested file name: `<repo name>-v<version>-bundle.zip`\n\n")
	} else {
		w("- Target repository: `%s`\n", repo)
		if prev != "" {
			w("- Previous version: `%s` → this version must be higher (suggested: `%s`)\n", prev, ver)
		} else {
			w("- First release (suggested version: `%s`)\n", ver)
		}
		w("- Suggested file name: `%s-v%s-bundle.zip`\n\n", name, ver)
	}
	w("## Layout\n\n```\n")
	w("%s-v%s-bundle.zip\n", name, ver)
	w("├─ release.json             required. Release information\n")
	w("├─ README.md                required. First language in languages\n")
	w("├─ README.<code>.md         required for every other language (e.g. README.ko.md)\n")
	w("├─ RELEASE_NOTES.md         required. First language in languages\n")
	w("├─ RELEASE_NOTES.<code>.md  required for every other language\n")
	w("├─ src/                     required. Full source committed to the repository\n")
	w("└─ dist/                    optional. Build outputs attached to the release\n```\n\n")
	w("- The items above must sit at the top of the zip. (A zip that wraps everything in one folder is also accepted.)\n")
	w("- README and RELEASE_NOTES files are committed to the repository **root**. Do not put files with the same names inside `src/`.\n\n")
	w("## release.json\n\n```json\n%s\n```\n\n", example)
	w("| Field | Required | Description |\n| --- | --- | --- |\n")
	w("| spec_version | yes | Always `%d` |\n", SpecVersion)
	w("| repo | yes | `owner/name`. Must be `%s` |\n", repo)
	w("| version | yes | Two numbers (`0.1`) or three (`0.1.1`). No leading v |\n")
	w("| tag | no | Defaults to `v` + version |\n")
	w("| title | no | Release title. Defaults to the tag |\n")
	w("| commit_message | no | Defaults to `Release <tag>` |\n")
	w("| languages | yes | Document language codes. The first is the main language (e.g. `[\"en\", \"ko\"]`) |\n")
	w("| assets | no | Paths inside `dist/` to attach to the release. Use `[]` if none |\n\n")
	w("Do not add any other keys to release.json.\n\n")
	w("## Version rules\n\n")
	w("- Format: `major.minor` or `major.minor.patch` (e.g. `0.1`, `0.2`, `1.0.3`). Digits only.\n")
	w("- Must be higher than the previous version, compared numerically (`0.10` > `0.9`, `0.1` = `0.1.0`).\n")
	w("- A tag or release that already exists is rejected.\n\n")
	w("## Language rules\n\n")
	w("- Write the README and release notes in every language listed in `languages`.\n")
	w("- The first language uses `README.md` / `RELEASE_NOTES.md`; the others use `README.<code>.md` / `RELEASE_NOTES.<code>.md`.\n")
	w("- Put links to the other language READMEs at the very top of each README. Example: `English | [한국어](README.ko.md)`\n")
	w("- Release notes are joined in order into one release description.\n\n")
	w("## README rules\n\n")
	w("1. Top: language links\n")
	w("2. First paragraph: **why this program exists** (what it does and why)\n")
	w("3. Then: requirements (supported OS and hardware), installation and usage, license\n\n")
	w("## src/ rules\n\n")
	w("- `src/` is a **full snapshot** of the repository. Files missing from `src/` are **deleted** from the repository. Never include only part of it.\n")
	w("- No build outputs, caches or temporary files (things `.gitignore` should cover).\n")
	w("- No `.git` folder and no symbolic links.\n")
	w("- Each file must be under 100 MB (over 50 MB triggers a warning).\n")
	w("- No file names that differ only by letter case.\n\n")
	w("## dist/ rules\n\n")
	w("- Only put downloadable executables or packages here, and list their paths in `assets`.\n")
	w("- Asset file names must be unique. They do not need to contain the version.\n\n")
	w("## Forbidden (upload is rejected)\n\n")
	w("- Secrets such as tokens, API keys, passwords, private keys (`ghp_`, `github_pat_`, `sk-`, `-----BEGIN … PRIVATE KEY-----`, …)\n")
	w("- Secret files like `.env`, `*.pem`, `*.key`, `*.pfx`, `id_rsa` (`.env.example` is allowed)\n")
	w("- Zip paths containing `../` or absolute paths\n")
	w("- Adding or changing GitHub Actions workflows (`.github/workflows/`) unless the user asked for it (the user must approve them separately)\n\n")
	w("## Checklist before handing over\n\n")
	if unknown {
		w("- [ ] release.json repo is the address the user confirmed (not the OWNER/REPO placeholder)\n")
	} else {
		w("- [ ] release.json repo is `%s` and version is higher than the previous one\n", repo)
	}
	w("- [ ] README and RELEASE_NOTES exist in every language in languages\n")
	w("- [ ] The first README paragraph explains why the program exists\n")
	w("- [ ] src/ contains the complete repository source\n")
	w("- [ ] Every file listed in assets exists in dist/\n")
	w("- [ ] No secrets or secret files\n")
	w("- [ ] Delivered as a single zip file\n")
	return b.String()
}
