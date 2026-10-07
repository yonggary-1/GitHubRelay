## GitHub Relay v0.3

- The New release tab is now the first tab and opens at startup; Repositories moved to the last tab
- New release tab redesigned:
  - Registered repositories are shown in a scrollable list with latest version and token status
  - Drop area with a Browse button; once a bundle is chosen it turns into the check list
  - Reset (clears the bundle) and Check again buttons, enabled only while a bundle is loaded
  - The bundle file name is shown in the summary line
- Dropping a bundle selects the repository named in its release.json automatically
- Choosing another repository yourself resets the check area
- The last selected repository is remembered
- With no repository registered, the drop area offers a button to register one
- Versions are compared only with releases on GitHub; an already released version shows a single clear message
- Confirmation checkboxes stay disabled while any check has failed

_Unofficial tool. Not made, endorsed, or supported by GitHub, Inc._
