# Install codex-tether under %LOCALAPPDATA%\codex-tether and point the Codex
# desktop app at it through the user-level CODEX_CLI_PATH.
#
#   irm https://raw.githubusercontent.com/gcol33/codex-tether/main/install.ps1 | iex
#       install the latest release
#   .\install.ps1              from a checkout: use codex-tether.exe next to this
#                              script, or build it with Go, or download the release
#   .\install.ps1 -Uninstall   remove CODEX_CLI_PATH, the PATH entry and the binary
#
# Restart the Codex app afterwards. Run from a normal (non-elevated) PowerShell.
param([switch]$Uninstall)

# The body runs in its own scope, so settings and functions do not stay
# behind in the session that piped this script into iex.
& {
    $ErrorActionPreference = 'Stop'
    $repo = 'gcol33/codex-tether'
    $dst = Join-Path $env:LOCALAPPDATA 'codex-tether'
    $exe = Join-Path $dst 'codex-tether.exe'

    function Set-UserPath([bool]$Add) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $raw = [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
            $parts = @(($raw -split ';') | Where-Object { $_ -and $_ -ne $dst })
            if ($Add) { $parts += $dst }
            $key.SetValue('Path', ($parts -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
        } finally { $key.Close() }
    }

    function Send-EnvironmentChange {
        if (-not ('CodexTether.Win32' -as [type])) {
            Add-Type -Namespace CodexTether -Name Win32 -MemberDefinition '[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessageTimeout(IntPtr h, uint m, UIntPtr w, string l, uint f, uint t, out UIntPtr r);'
        }
        $r = [UIntPtr]::Zero
        [CodexTether.Win32]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$r) | Out-Null
    }

    # Download the latest release binary for this machine and check it
    # against the release's SHA256SUMS.
    function Get-ReleaseBinary([string]$OutFile) {
        $arch = $env:PROCESSOR_ARCHITEW6432
        if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
        $goarch = switch ($arch) { 'AMD64' { 'amd64' } 'ARM64' { 'arm64' } default { throw "unsupported processor architecture $arch" } }
        $name = "codex-tether-windows-$goarch.exe"
        $base = "https://github.com/$repo/releases/latest/download"
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -UseBasicParsing "$base/$name" -OutFile $OutFile
        $sums = (Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS").Content
        if ($sums -is [byte[]]) { $sums = [Text.Encoding]::ASCII.GetString($sums) }
        $want = $null
        foreach ($line in ($sums -split "`n")) {
            $f = $line.Trim() -split '\s+'
            if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq $name) { $want = $f[0] }
        }
        if (-not $want) { throw "SHA256SUMS has no entry for $name" }
        if ((Get-FileHash -Algorithm SHA256 $OutFile).Hash -ne $want.ToUpperInvariant()) {
            Remove-Item $OutFile -Force
            throw "checksum mismatch for $name"
        }
    }

    # Binaries replaced while an app still ran them; each is deleted once
    # nothing has it open.
    function Remove-OldBinaries {
        Get-ChildItem $dst -Filter 'codex-tether.exe.*.old' -ErrorAction SilentlyContinue |
            ForEach-Object { Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue }
    }

    if ($Uninstall) {
        [Environment]::SetEnvironmentVariable('CODEX_CLI_PATH', $null, 'User')
        Set-UserPath $false
        Send-EnvironmentChange
        if (Test-Path $exe) { Remove-Item $exe -Force -ErrorAction SilentlyContinue }
        Remove-OldBinaries
        Write-Output 'codex-tether removed. Quit the Codex app, sign out and back in, then start the app again.'
        return
    }

    New-Item -ItemType Directory -Force $dst | Out-Null
    Remove-OldBinaries
    $src = $null
    if ($PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot 'go.mod'))) {
        $src = Join-Path $PSScriptRoot 'codex-tether.exe'
        if (-not (Test-Path $src)) {
            if (Get-Command go -ErrorAction SilentlyContinue) {
                Push-Location $PSScriptRoot
                try { go build -trimpath -ldflags '-s -w' -o codex-tether.exe . } finally { Pop-Location }
                if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
            } else { $src = $null }
        }
    }
    if (-not $src) {
        $src = Join-Path $dst 'codex-tether.download'
        Get-ReleaseBinary $src
    }

    if (-not (Test-Path $exe) -or (Get-FileHash $src).Hash -ne (Get-FileHash $exe).Hash) {
        # A running exe cannot be overwritten but can be renamed; the app keeps
        # the old file until it restarts.
        if (Test-Path $exe) { Move-Item $exe ("$exe." + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss') + '.old') }
        Copy-Item $src $exe
    }
    if ($src -like '*.download') { Remove-Item $src -Force }
    [Environment]::SetEnvironmentVariable('CODEX_CLI_PATH', $exe, 'User')
    Set-UserPath $true
    Send-EnvironmentChange
    Write-Output "Installed $exe, set CODEX_CLI_PATH and added $dst to your PATH."
    Write-Output 'Quit the Codex app and start it again. If it still runs its own server, sign out and back in so Explorer picks up the variable.'
}
