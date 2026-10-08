## GitHub Relay v0.6

- Batch release: drop several bundle zips at once (or select several in Browse…) to release them one after another
  - Every bundle is checked locally first (format, secrets, same repository, duplicate versions); nothing is uploaded if any fails
  - Bundles are released in version order (`0.9` before `0.10`), with a notice when file names suggest a different order
  - One confirmation starts the batch; each bundle is checked against GitHub right before its upload
  - The batch stops at the first failure; bundles with warnings ask for confirmation, and declining pauses the batch
  - Stop and Resume buttons; versions already on GitHub are skipped, so a stopped batch can be dropped again
  - Double-click a bundle in the list to see its full check results
- Batch jobs always publish immediately; the draft checkbox says it is disabled for batch jobs

_Unofficial tool. Not made, endorsed, or supported by GitHub, Inc._
