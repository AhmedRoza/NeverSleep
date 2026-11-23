#!/usr/bin/env bash
# Builds NeverSleep.exe for Windows (cross-compiles from Linux/macOS/Windows with Go 1.22+).
set -euo pipefail
go install github.com/tc-hib/go-winres@latest
"$(go env GOPATH)/bin/go-winres" make
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o NeverSleep.exe .
sha256sum NeverSleep.exe | awk '{print $1"  NeverSleep.exe"}' > NeverSleep.exe.sha256
echo "Built NeverSleep.exe and NeverSleep.exe.sha256 (upload both to a GitHub release)"
