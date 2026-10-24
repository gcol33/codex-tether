# Build codex-share, install it under %LOCALAPPDATA%\codex-share and point the
# Codex desktop app at it via the user-level CODEX_CLI_PATH. Restart the app
# afterwards. Uninstall: .\install.ps1 -Uninstall
param([switch]$Uninstall)
$ErrorActionPreference = 'Stop'
$dst = Join-Path $env:LOCALAPPDATA 'codex-share'

if ($Uninstall) {
    [Environment]::SetEnvironmentVariable('CODEX_CLI_PATH', $null, 'User')
    Write-Output 'CODEX_CLI_PATH removed; restart the Codex app.'
    return
}

Push-Location $PSScriptRoot
try { go build -ldflags '-s -w' -o codex-share.exe . } finally { Pop-Location }
New-Item -ItemType Directory -Force $dst | Out-Null
Copy-Item (Join-Path $PSScriptRoot 'codex-share.exe') (Join-Path $dst 'codex-share.exe') -Force
[Environment]::SetEnvironmentVariable('CODEX_CLI_PATH', (Join-Path $dst 'codex-share.exe'), 'User')
Write-Output "installed $dst\codex-share.exe; restart the Codex app."
