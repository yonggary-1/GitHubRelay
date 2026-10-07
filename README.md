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
- Not supported: Windows 7, 8 and 8.1

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

## Renaming a repository

If you rename or transfer a repository on GitHub, GitHub Relay notices it on the next check or verification and offers to update the registration. The token, history and settings are kept. You can also type the new address and press **Change address** on the Repositories tab; this only works for the same repository. Bundles that still use the former name are accepted with a warning.

## Building from source

Requires Go 1.23 or newer. On any OS:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GithubRelay.exe .
```

`rsrc_windows_amd64.syso` holds the icon, manifest and version information; `build.sh` shows how to regenerate it. Run `go test ./...` for the core tests (they use a local fake GitHub server).

## License

MIT License. See [LICENSE](LICENSE).
