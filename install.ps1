# Install codex-tether under %LOCALAPPDATA%\codex-tether and point the Codex
# desktop app at it through the user-level CODEX_CLI_PATH.
#
#   .\install.ps1              install (uses codex-tether.exe next to this script,
#                              or builds it with Go when absent)
#   .\install.ps1 -Uninstall   remove CODEX_CLI_PATH and the installed binary
#
# Restart the Codex app afterwards. Run from a normal (non-elevated) PowerShell.
param([switch]$Uninstall)
$ErrorActionPreference = 'Stop'
$dst = Join-Path $env:LOCALAPPDATA 'codex-tether'
$exe = Join-Path $dst 'codex-tether.exe'

function Send-EnvironmentChange {
    Add-Type -Namespace CodexTether -Name Win32 -MemberDefinition '[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessageTimeout(IntPtr h, uint m, UIntPtr w, string l, uint f, uint t, out UIntPtr r);'
    $r = [UIntPtr]::Zero
    [CodexTether.Win32]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$r) | Out-Null
}

if ($Uninstall) {
    [Environment]::SetEnvironmentVariable('CODEX_CLI_PATH', $null, 'User')
    Send-EnvironmentChange
    if (Test-Path $exe) { Remove-Item $exe -Force -ErrorAction SilentlyContinue }
    Write-Output 'CODEX_CLI_PATH removed. Restart the Codex app (sign out and in if it was started from the Start menu).'
    return
}

$src = Join-Path $PSScriptRoot 'codex-tether.exe'
if (-not (Test-Path $src)) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'codex-tether.exe not found next to install.ps1 and Go is not installed to build it.'
    }
    Push-Location $PSScriptRoot
    try { go build -ldflags '-s -w' -o codex-tether.exe . } finally { Pop-Location }
}

New-Item -ItemType Directory -Force $dst | Out-Null
Copy-Item $src $exe -Force
[Environment]::SetEnvironmentVariable('CODEX_CLI_PATH', $exe, 'User')
Send-EnvironmentChange
Write-Output "Installed $exe and set CODEX_CLI_PATH."
Write-Output 'Quit the Codex app and start it again. If it still runs its own server, sign out and back in so Explorer picks up the variable.'
