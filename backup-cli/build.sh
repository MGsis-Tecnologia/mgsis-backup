#!/usr/bin/env bash
# Gera os 3 executáveis em ./dist (Go 1.24+; sem dependências externas).
set -euo pipefail
cd "$(dirname "$0")"
go vet ./...
mkdir -p dist
GOOS=linux   GOARCH=amd64 go build -o dist/mgsis-backup-linux-amd64 .
GOOS=linux   GOARCH=386   go build -o dist/mgsis-backup-linux-386 .
GOOS=windows GOARCH=amd64 go build -o dist/mgsis-backup-windows-amd64.exe .
ls -la dist
