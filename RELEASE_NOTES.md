## GitHub Relay v1.0

The first stable release. GitHub Relay publishes projects made in AI chat sessions to GitHub without ever giving a token to the chat session: the session produces a release bundle, and GitHub Relay checks it on your PC and uploads it.

**What it does**

- **Repositories**: register each repository with its own fine-grained token; access, write permission and expiry are verified, tokens are encrypted with your Windows account
- **New release**: drop a bundle zip to check it (format, secrets, unsafe paths, versions, bundles meant for another project) and see every added, changed and deleted file before you approve the upload
- **Batch release**: drop several bundles to release them one after another in version order
- **History**: every upload, sync with GitHub, retry a release whose commit already landed
- **Fixing releases**: revert the code to an earlier version (as a new commit), delete releases, or both together
- **Bundle spec**: a spec document in Korean and English to give to chat sessions
- **Repository renames** are detected and followed without losing history
- **Self-update** from this repository, with verification and automatic restart
- Korean / English interface; one portable exe plus one data file; Windows 10/11 x64

**Changes since v0.9**

- Version 1.0; no functional changes
- The design document is now included in the repository: `docs/DESIGN.ko.md` (Korean)

_Unofficial tool. Not made, endorsed, or supported by GitHub, Inc._
