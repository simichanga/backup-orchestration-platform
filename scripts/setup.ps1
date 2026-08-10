<#
.SYNOPSIS
    Checks that everything BOP's build/dev/demo workflow needs is
    actually installed and working, pinpointing exactly which tool is
    missing (or misconfigured) and how to fix it - rather than letting
    `make build`/`make demo` fail three layers deep with a confusing
    error. Also scaffolds config.yaml/inventory.yaml/data/ from the
    committed *.example.yaml templates if they don't exist yet, so
    `make run`/`make dev` have something real to point at immediately.

    Safe to re-run any time: every check is read-only, and the scaffolding
    step never overwrites a file that already exists.
#>

$RepoRoot = Split-Path -Parent $PSScriptRoot
$results = New-Object System.Collections.Generic.List[object]

function Add-Result($Name, $Ok, $Detail, $Required, $Hint) {
    $results.Add([pscustomobject]@{
        Name     = $Name
        Ok       = $Ok
        Detail   = $Detail
        Required = $Required
        Hint     = $Hint
    })
}

# Same reasoning as scripts/try-it-out.ps1's Require-Command: PATH
# resolution for a couple of well-known Windows tools has been observed
# to differ between an interactive terminal and a non-interactive parent
# (a background job, `make`) even for the same user on the same machine.
$FallbackToolPaths = @{
    'ssh-keygen' = "$env:WINDIR\System32\OpenSSH\ssh-keygen.exe"
}

function Resolve-Tool($Name) {
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $fallback = $FallbackToolPaths[$Name]
    if ($fallback -and (Test-Path $fallback)) { return $fallback }
    return $null
}

Write-Host "==> Checking required tools (build/run/dev)" -ForegroundColor Cyan

$goPath = Resolve-Tool 'go'
if ($goPath) {
    $goVersionOut = & $goPath version
    Add-Result 'Go' $true $goVersionOut $true $null
} else {
    Add-Result 'Go' $false $null $true 'Install from https://go.dev/dl/, then reopen your terminal.'
}

$nodePath = Resolve-Tool 'node'
if ($nodePath) {
    $nodeVersion = & $nodePath --version
    Add-Result 'Node.js' $true $nodeVersion $true $null
} else {
    Add-Result 'Node.js' $false $null $true 'Install from https://nodejs.org/ (npm comes with it), then reopen your terminal.'
}

$npmPath = Resolve-Tool 'npm'
if ($npmPath) {
    $npmVersion = & npm --version 2>$null
    Add-Result 'npm' $true "v$npmVersion" $true $null
} else {
    Add-Result 'npm' $false $null $true 'Comes with Node.js - install from https://nodejs.org/, then reopen your terminal.'
}

$gitPath = Resolve-Tool 'git'
if ($gitPath) {
    Add-Result 'Git' $true (& $gitPath --version) $true $null
} else {
    Add-Result 'Git' $false $null $true 'Install from https://git-scm.com/downloads, then reopen your terminal.'
}

Write-Host "==> Checking extras (needed for make demo / real backups)" -ForegroundColor Cyan

$dockerPath = Resolve-Tool 'docker'
if (-not $dockerPath) {
    Add-Result 'Docker' $false $null $false 'Install Docker Desktop: https://www.docker.com/products/docker-desktop/ (only needed for make demo).'
} else {
    $dockerServerVersion = & docker version --format '{{.Server.Version}}' 2>$null
    if ($LASTEXITCODE -eq 0 -and $dockerServerVersion) {
        Add-Result 'Docker' $true "engine $dockerServerVersion" $false $null
    } else {
        Add-Result 'Docker' $false 'installed, daemon not responding' $false 'Docker is installed but its daemon is not running - start Docker Desktop and wait for it to finish starting (only needed for make demo).'
    }
}

$resticPath = Resolve-Tool 'restic'
if ($resticPath) {
    Add-Result 'restic' $true (& $resticPath version) $false $null
} else {
    Add-Result 'restic' $false $null $false 'Install from https://restic.net/ and put restic.exe on your PATH (only needed to actually run a backup, not to build).'
}

$sshKeygenPath = Resolve-Tool 'ssh-keygen'
if ($sshKeygenPath) {
    Add-Result 'ssh-keygen' $true $sshKeygenPath $false $null
} else {
    Add-Result 'ssh-keygen' $false $null $false "Turn on the 'OpenSSH Client' optional feature in Windows Settings (only needed for make demo)."
}

Write-Host ""
Write-Host "==> Results" -ForegroundColor Cyan
$anyRequiredMissing = $false
foreach ($r in $results) {
    if ($r.Ok) {
        $mark = "[OK]  "
        $color = 'Green'
        $line = "$mark$($r.Name.PadRight(11)) $($r.Detail)"
    } else {
        $mark = if ($r.Required) { "[FAIL] " } else { "[--]  " }
        $color = if ($r.Required) { 'Red' } else { 'Yellow' }
        $line = "$mark$($r.Name.PadRight(11)) not found"
        if ($r.Required) { $anyRequiredMissing = $true }
    }
    Write-Host $line -ForegroundColor $color
    if (-not $r.Ok -and $r.Hint) {
        Write-Host "       -> $($r.Hint)" -ForegroundColor DarkGray
    }
}

if ($anyRequiredMissing) {
    Write-Host ""
    Write-Host "Missing required tools above - fix those first, then re-run .\scripts\setup.ps1." -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "==> Setting up config.yaml / inventory.yaml / data/" -ForegroundColor Cyan

function Copy-IfMissing($From, $To) {
    if (Test-Path $To) {
        Write-Host "  $To already exists - left untouched." -ForegroundColor DarkGray
        return
    }
    Copy-Item $From $To
    Write-Host "  Created $To from $(Split-Path -Leaf $From)" -ForegroundColor Green
}

function New-RandomToken {
    -join ((48..57) + (65..90) + (97..122) | Get-Random -Count 32 | ForEach-Object { [char]$_ })
}

function Write-IfMissing($Path, [string]$Content) {
    if (Test-Path $Path) {
        Write-Host "  $Path already exists - left untouched." -ForegroundColor DarkGray
        return
    }
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $utf8NoBom)
    Write-Host "  Created $Path" -ForegroundColor Green
}

Push-Location $RepoRoot
try {
    New-Item -ItemType Directory -Path 'data' -Force | Out-Null

    Copy-IfMissing 'config.example.yaml' 'config.yaml'
    Copy-IfMissing 'inventory.example.yaml' 'inventory.yaml'

    # Matches config.example.yaml's data/ paths - a real restic password
    # and real bearer tokens, generated locally, never committed (data/ is
    # gitignored). Not a substitute for docs/03-getting-started/
    # configuration.md#secrets-management in production.
    Write-IfMissing 'data\restic-password.txt' (New-RandomToken)
    Write-IfMissing 'data\api-tokens.txt' (New-RandomToken)
    Write-IfMissing 'data\api-write-tokens.txt' (New-RandomToken)
    Write-IfMissing 'data\known_hosts' ''
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "==> Ready" -ForegroundColor Green
Write-Host "  make run   - build and run against config.yaml (the placeholder inventory host won't actually connect - edit inventory.yaml with a real one, see docs/03-getting-started/inventory-reference.md)"
Write-Host "  make dev   - backend + frontend dev server together, hot-reloading"
Write-Host "  make demo  - a real, fully working demo against a throwaway Docker target, no editing required"
Write-Host ""
Write-Host "  Read tokens:  type data\api-tokens.txt"
Write-Host "  Write tokens: type data\api-write-tokens.txt"
