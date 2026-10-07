#!/bin/bash
# Builds GithubRelay.exe. Run on Linux/macOS/WSL with Go 1.23+.
# The resource object (icon, manifest, version info) is committed as
# rsrc_windows_amd64.syso. To regenerate it you need mingw-w64 windres:
#   printf '#!/bin/bash\ncat "${@: -1}"\n' > /tmp/pp.sh && chmod +x /tmp/pp.sh
#   (cd res && x86_64-w64-mingw32-windres --preprocessor=/tmp/pp.sh -O coff -i app.rc -o ../rsrc_windows_amd64.syso)
set -e
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GithubRelay.exe .
echo "built GithubRelay.exe"
