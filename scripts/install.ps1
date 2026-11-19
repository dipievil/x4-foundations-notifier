#Requires -Version 5.1
<#
.SYNOPSIS
    Installs the Fleet Order Notifier extension into a Steam copy of
    X4: Foundations (Windows).

.DESCRIPTION
    Finds the game, asks you to confirm the folder, checks that the djfhe_http
    dependency is installed, copies fleet_notifier\ into extensions\ and can set
    the shared token in the Lua file.

.PARAMETER GamePath
    X4 install folder (the one containing X4.exe). Skips the automatic search.

.PARAMETER Token
    Shared token written into ui\notifier.lua and .env. Must match X4_TOKEN.

.PARAMETER InstallDir
    Where the server files go (default: %LOCALAPPDATA%\x4-notifier).

.PARAMETER Yes
    Accept every confirmation (for unattended runs).

.EXAMPLE
    .\install.ps1

.EXAMPLE
    .\install.ps1 -GamePath 'D:\Steam\steamapps\common\X4 Foundations' -Yes
#>
[CmdletBinding()]
param(
    [string]$GamePath,
    [string]$Token,
    [string]$InstallDir,
    [switch]$Yes
)

$ErrorActionPreference = 'Stop'

$root       = Split-Path -Parent $PSScriptRoot
$sourceDir  = Join-Path $root 'fleet_notifier'

function Write-Say {
    param([string]$Message)
    Write-Host $Message
}

function Write-Warn {
    param([string]$Message)
    Write-Host "WARNING: $Message" -ForegroundColor Yellow
}

function Stop-Install {
    param([string]$Message)
    Write-Warn $Message
    exit 1
}

function Confirm-Step {
    param([string]$Message)
    if ($Yes) { return $true }
    $answer = Read-Host "$Message [Y/n]"
    if ($answer -match '^(n|no)$') { return $false }
    return $true
}

function Get-SteamRoots {
    $roots = New-Object System.Collections.Generic.List[string]

    $keys = @(
        'HKCU:\Software\Valve\Steam',
        'HKLM:\SOFTWARE\WOW6432Node\Valve\Steam',
        'HKLM:\SOFTWARE\Valve\Steam'
    )
    foreach ($key in $keys) {
        try {
            $props = Get-ItemProperty -Path $key -ErrorAction Stop
            $value = $props.SteamPath
            if (-not $value) { $value = $props.InstallPath }
            if ($value) { $roots.Add(($value -replace '/', '\')) }
        } catch {
            # Not every install has these keys; that is fine.
        }
    }

    $known = New-Object System.Collections.Generic.List[string]
    if (${env:ProgramFiles(x86)}) { $known.Add((Join-Path ${env:ProgramFiles(x86)} 'Steam')) }
    if ($env:ProgramFiles) { $known.Add((Join-Path $env:ProgramFiles 'Steam')) }
    $known.Add('C:\Program Files (x86)\Steam')
    foreach ($path in $known) {
        if (Test-Path -LiteralPath $path) { $roots.Add($path) }
    }

    return $roots | Select-Object -Unique
}

function Get-SteamLibraries {
    param([string]$SteamRoot)

    $libraries = New-Object System.Collections.Generic.List[string]
    $libraries.Add($SteamRoot)

    $vdf = Join-Path (Join-Path $SteamRoot 'steamapps') 'libraryfolders.vdf'
    if (Test-Path -LiteralPath $vdf) {
        foreach ($line in Get-Content -LiteralPath $vdf) {
            # New format: "path" "D:\\Games\\Steam"; old format: "1" "D:\\Games\\Steam"
            if ($line -match '^\s*"(path|\d+)"\s+"([^"]+)"') {
                $libraries.Add(($Matches[2] -replace '\\\\', '\'))
            }
        }
    }

    return $libraries | Select-Object -Unique
}

function Find-X4 {
    $candidates = New-Object System.Collections.Generic.List[string]

    if ($GamePath) {
        $candidates.Add($GamePath)
    } else {
        foreach ($steamRoot in Get-SteamRoots) {
            foreach ($library in Get-SteamLibraries $steamRoot) {
                $candidates.Add((Join-Path (Join-Path (Join-Path $library 'steamapps') 'common') 'X4 Foundations'))
            }
        }
    }

    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath (Join-Path $candidate 'X4.exe')) { return $candidate }
    }
    return $null
}

# --- 1. locate the game -----------------------------------------------------

Write-Say 'X4 Fleet Order Notifier installer'
Write-Say ''

if ($GamePath -and -not (Test-Path -LiteralPath (Join-Path $GamePath 'X4.exe'))) {
    Stop-Install "'$GamePath' does not look like an X4 install (no X4.exe there)."
}

$game = Find-X4
if ($game -and (Test-Path -LiteralPath (Join-Path $game 'X4.exe'))) {
    Write-Say 'X4: Foundations found at:'
    Write-Say "  $game"
    if (-not (Confirm-Step 'Install into this folder?')) { $game = $null }
} elseif ($game) {
    Write-Warn "'$game' does not look like an X4 install (no X4.exe there)."
    $game = $null
} else {
    Write-Warn 'Could not find X4: Foundations in a Steam library.'
    Write-Warn 'Only the Steam version is detected automatically.'
}

while (-not $game) {
    $entered = Read-Host 'Path to the X4 Foundations folder (the one containing X4.exe)'
    if (-not $entered) {
        Stop-Install 'No game folder confirmed; nothing was installed.'
    }
    $entered = $entered.Trim().Trim('"')
    if (Test-Path -LiteralPath (Join-Path $entered 'X4.exe')) {
        $game = $entered
    } else {
        Write-Warn "'$entered' does not look like an X4 install (no X4.exe there)."
    }
}

Write-Say ''
Write-Say "Game folder: $game"

# --- 2. check the dependency ------------------------------------------------

$extensionsDir = Join-Path $game 'extensions'
$djfheDir      = Join-Path $extensionsDir 'djfhe_http'

if (-not (Test-Path -LiteralPath (Join-Path $djfheDir 'content.xml'))) {
    Write-Warn 'djfhe_http is not installed in:'
    Write-Warn "  $djfheDir"
    Write-Warn ''
    Write-Warn 'Download it from https://github.com/djfhe/x4_http and unpack it so that'
    Write-Warn 'content.xml ends up in that folder (rename the extracted folder to djfhe_http).'
    Stop-Install 'Cannot continue without the djfhe_http extension.'
}

Write-Say 'djfhe_http: found'

# --- 3. copy the mod --------------------------------------------------------

if (-not (Test-Path -LiteralPath $sourceDir)) {
    Stop-Install "Cannot find $sourceDir. Run this script from a copy of the repository."
}

$destination = Join-Path $extensionsDir 'fleet_notifier'
if (Test-Path -LiteralPath $destination) {
    Write-Say ''
    Write-Say 'An earlier installation exists at:'
    Write-Say "  $destination"
    if (-not (Confirm-Step 'Replace it?')) {
        Stop-Install 'Nothing was installed.'
    }
    Remove-Item -LiteralPath $destination -Recurse -Force
}

if (-not (Test-Path -LiteralPath $extensionsDir)) {
    New-Item -ItemType Directory -Path $extensionsDir | Out-Null
}
Copy-Item -LiteralPath $sourceDir -Destination $destination -Recurse -Force
Write-Say "fleet_notifier: copied to $destination"

# --- 4. optional shared token ----------------------------------------------

Write-Say ''
if (-not $InstallDir) {
    if ($env:LOCALAPPDATA) {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'x4-notifier'
    } else {
        $InstallDir = Join-Path (Join-Path $HOME '.local/share') 'x4-notifier'
    }
}

$envFile = Join-Path $InstallDir '.env'
$existingToken = ''
if (Test-Path -LiteralPath $envFile) {
    foreach ($line in Get-Content -LiteralPath $envFile) {
        if ($line -match '^\s*X4_TOKEN\s*=\s*(.*)$') { $existingToken = $Matches[1].Trim(); break }
    }
}

if (-not $Token -and -not $Yes) {
    if ($existingToken) {
        $entered = Read-Host 'Shared token for the local server (empty keeps the current one)'
    } else {
        $entered = Read-Host 'Shared token for the local server (must match X4_TOKEN in .env, empty for none)'
    }
    if ($entered) { $Token = $entered }
}

# The Lua side and .env must always carry the same token: a leftover template
# value on one side makes every event fail with 401 and the server logs nothing.
$effectiveToken = $Token
if (-not $effectiveToken) { $effectiveToken = $existingToken }
if ($effectiveToken -and $effectiveToken -notmatch '^[A-Za-z0-9._-]+$') {
    Write-Warn 'The token has characters the script will not write (allowed: A-Z a-z 0-9 . _ -).'
    Write-Warn "Set it by hand in $(Join-Path (Join-Path $destination 'ui') 'notifier.lua') and in $envFile"
    Write-Warn 'Leaving both sides without a token so they still match.'
    $effectiveToken = ''
}

$luaPath = Join-Path (Join-Path $destination 'ui') 'notifier.lua'
$content = Get-Content -LiteralPath $luaPath -Raw
$content = [regex]::Replace($content, '(?m)^local TOKEN = ".*"$', ('local TOKEN = "{0}"' -f $effectiveToken))
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($luaPath, $content, $utf8)
if ($effectiveToken) {
    if ($Token) {
        Write-Say 'Token: written to ui\notifier.lua'
    } else {
        Write-Say 'Token: kept the X4_TOKEN from the existing .env in ui\notifier.lua'
    }
} else {
    Write-Say 'Token: none (both sides empty)'
}

# --- 5. deploy the server folder -------------------------------------------

Write-Say ''
Write-Say 'Server folder:'
Write-Say "  $InstallDir"

$deployServer = Confirm-Step 'Copy the server files there?'
if (-not $deployServer) {
    Write-Warn 'Server files not copied; only the extension was installed.'
} else {
    if (-not (Test-Path -LiteralPath $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $serverFiles = @('x4-notifier-windows-amd64.exe', 'start-x4-notifier.bat')
    foreach ($name in $serverFiles) {
        $from = Join-Path (Join-Path $root 'bin') $name
        if (Test-Path -LiteralPath $from) {
            Copy-Item -LiteralPath $from -Destination (Join-Path $InstallDir $name) -Force
        } else {
            Write-Warn "bin\$name is missing; copy it yourself."
        }
    }
    Write-Say 'Server files: copied'

    $envSample = Join-Path (Join-Path $root 'bin') '.env.example'
    if (-not (Test-Path -LiteralPath $envFile)) {
        if (Test-Path -LiteralPath $envSample) {
            $lines = Get-Content -LiteralPath $envSample
            $lines = $lines -replace '^X4_TOKEN=.*$', ('X4_TOKEN={0}' -f $effectiveToken)
            $utf8 = New-Object System.Text.UTF8Encoding($false)
            [System.IO.File]::WriteAllLines($envFile, $lines, $utf8)
            Write-Say "Configuration: created $envFile"
        } else {
            Write-Warn "bin\.env.example is missing; create $envFile yourself."
        }
    } else {
        # Only X4_TOKEN is rewritten: the installer just replaced the extension
        # with a fresh copy, so the two sides have to be brought back in sync.
        $lines = Get-Content -LiteralPath $envFile
        $patched = $lines -replace '^(\s*X4_TOKEN\s*=\s*).*$', ('${1}' + $effectiveToken)
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [System.IO.File]::WriteAllLines($envFile, $patched, $utf8)
        Write-Say "Configuration: kept the existing $envFile (X4_TOKEN synced with the extension)"
    }
}

# --- 6. what is left --------------------------------------------------------

Write-Say ''
Write-Say 'Done. Installed to:'
Write-Say "  $destination"
if ($deployServer) {
    Write-Say 'Server folder:'
    Write-Say "  $InstallDir"
}
Write-Say ''
Write-Say 'Next steps:'
Write-Say "  1. In X4: Settings > Extensions, enable 'djfhe http' and 'Fleet Order Notifier'."
Write-Say '  2. In X4: Settings > Game Settings, turn Protected UI Mode OFF.'
if ($deployServer) {
    Write-Say "  3. Edit $(Join-Path $InstallDir '.env') with your notification app values"
    Write-Say '     (see docs\notification-apps-guide.md).'
    Write-Say '  4. Double-click start-x4-notifier.bat in the server folder and keep it running'
    Write-Say '     while you play.'
} else {
    Write-Say '  3. Rename bin\.env.example to bin\.env, fill in the notification app values'
    Write-Say '     (see docs\notification-apps-guide.md) and run bin\start-x4-notifier.bat.'
}
