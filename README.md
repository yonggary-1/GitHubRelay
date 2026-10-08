English | [한국어](README.ko.md)

> [!WARNING]
> **Unofficial tool.** GitHub Relay is not made, endorsed, or supported by GitHub, Inc. "GitHub" is a trademark of GitHub, Inc.

# GitHub Relay

GitHub Relay publishes projects developed in AI chat sessions to GitHub **without ever handing a GitHub token to the chat session**. Chat sessions often cannot reach GitHub, and a token pasted into a chat can leak and be abused later. With GitHub Relay, the chat session only produces a release bundle (a zip) that follows a fixed spec; this program checks it on your PC and then creates the commit and the release on GitHub. Your tokens never leave your PC.

Project page: https://github.com/yonggary-1/GitHubRelay

## How it works

1. **Register a repository** with a fine-grained token that can only write to that one repository.
2. Copy the **bundle spec** from the program and paste it into your chat session.
3. The chat session builds your project and gives you a bundle zip.
4. **Drop the zip** onto GitHub Relay. It checks the bundle and shows exactly which files will be added, changed or deleted.
5. Press **Approve & upload**. GitHub Relay creates the commit, the release and its attachments.

## Requirements

- Windows 10 or Windows 11, 64-bit (x64)
- Nothing to install: no .NET, no Visual C++ runtime, no git
- Not supported: Windows 7, 8 and 8.1. The Go runtime the program is built with requires Windows 10 or later, so the program cannot start on older versions.
- ARM64 Windows: no native build yet. Windows on ARM can usually run the x64 program through its built-in emulation, but this is untested. Native ARM64 and Windows 7/8 builds may be added once hardware is available for testing.

## Files

GitHub Relay is portable. Put `GithubRelay.exe` in a folder you can write to (for example Documents or Desktop, not Program Files). It creates one more file next to itself:

| File | Contents |
| --- | --- |
| `GithubRelay.exe` | The program |
| `GithubRelay.dat` | Settings, registered repositories, encrypted tokens, upload history |

Tokens are encrypted with your Windows account (DPAPI). If you copy both files to another PC, everything works except the tokens, which you enter again.

## Creating a token

GitHub → Settings → Developer settings → Fine-grained tokens → Generate new token

- Repository access: **Only select repositories**, pick the one repository
- Permissions: **Contents → Read and write** (nothing else)
- Expiration: always set one

## What is checked before upload

- Required files: `release.json`, README and release notes in every listed language, `src/`
- The bundle targets the selected repository, and the version is higher than the previous release
- No unsafe zip paths (`../`, absolute paths)
- No secrets (GitHub tokens, API keys, private keys, `.env` files)
- Attachments listed in `release.json` exist
- Changes to GitHub Actions workflows need a separate approval
- Deleting many files or very large files shows a warning

The bundle is treated as a full snapshot: files that are not in the bundle are removed from the repository. GitHub Relay never force-pushes; if the branch changed after the check, the upload stops without changing anything.

## Releasing many bundles at once (batch)

Drop several bundle zips at once, or select several in **Browse…**. GitHub Relay first checks every bundle locally (format, secrets, same repository, no duplicate versions) and lists them in version order, so `v0.9` comes before `v0.10` whatever the file names are. Press **Start batch** once; each bundle is then checked against GitHub and released in turn. The batch stops at the first failure, and asks before uploading a bundle whose check shows a warning. Versions that are already released are skipped, so a stopped batch can be dropped again and resumed. Batch jobs always publish immediately; the draft option is disabled.

## Fixing a release

On the History tab, select a version and use:

- **Revert to this version**: restores the repository *code* to that release by adding a new commit on top. Nothing is erased from the history and nothing is force-pushed. You see the list of files that will change before you confirm. Releases are not touched, so the "Latest" release stays the same. If newer releases exist, GitHub Relay asks whether to delete them too.
- **Delete release**: removes the release page, its attached files and its tag from GitHub. The code stays. When you delete the latest release, GitHub Relay offers to revert the code to the previous release at the same time.

A repository has two faces that move separately: the code (Code tab) and the releases (downloads). Reverting changes only the code; deleting changes only the releases. Doing both, in either order, undoes a bad release completely. A new version must always be higher than the highest release left on GitHub.

## Update check

At startup GitHub Relay checks the latest release of its own repository (no token needed) and tells you when a newer version exists. You can open the release page, be reminded next time, or skip that version. **Check for updates** at the bottom of the window checks on demand. GitHub Relay never downloads or replaces itself.

## Renaming a repository

If you rename or transfer a repository on GitHub, GitHub Relay notices it on the next check or verification and offers to update the registration. The token, history and settings are kept. You can also type the new address and press **Change address** on the Manage repositories tab; this only works for the same repository. Bundles that still use the former name are accepted with a warning.

## Building from source

Requires Go 1.23 or newer. On any OS:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GithubRelay.exe .
```

`rsrc_windows_amd64.syso` holds the icon, manifest and version information; `build.sh` shows how to regenerate it. Run `go test ./...` for the core tests (they use a local fake GitHub server).

## License

MIT License. See [LICENSE](LICENSE).
