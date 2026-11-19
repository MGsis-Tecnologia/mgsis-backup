# Gera os 3 executáveis em .\dist (Go 1.24+; sem dependências externas).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
go vet ./...
New-Item -ItemType Directory -Force dist | Out-Null
$env:GOOS="linux";   $env:GOARCH="amd64"; go build -o dist/mgsis-backup-linux-amd64 .
$env:GOOS="linux";   $env:GOARCH="386";   go build -o dist/mgsis-backup-linux-386 .
$env:GOOS="windows"; $env:GOARCH="amd64"; go build -o dist/mgsis-backup-windows-amd64.exe .
Remove-Item Env:GOOS, Env:GOARCH
Get-ChildItem dist
