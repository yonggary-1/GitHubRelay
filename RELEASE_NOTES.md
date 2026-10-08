## GitHub Relay v0.5

- The Bundle spec tab starts with "(none)" again, so a spec is never made for the wrong repository by accident
- A spec without a target marks `OWNER/REPO`, versions and file names as examples and tells the chat session to ask for the real values
- A spec for a selected repository says at the top that it is only for that repository
- New safety check: a bundle that shares almost no files with the target repository (other than README, LICENSE and similar) is blocked as possibly belonging to another project

_Unofficial tool. Not made, endorsed, or supported by GitHub, Inc._
