<#
.SYNOPSIS
    Runs the backend and the frontend dev server together: the backend
    serves the real API against a real config, and the frontend dev
    server (Vite) proxies /v1 to it (see web/vite.config.ts) and
    hot-reloads on every save - no rebuilding the whole embedded binary
    just to see a UI change. Both processes' output interleaves in this
    same terminal; Ctrl+C stops both cleanly.

    For a zero-config real demo instead (no config to write by hand), use
    scripts/try-it-out.ps1.

.PARAMETER Config
    Path to a real config.yaml with api.enabled: true. Required - there's
    no default, since a real config is deployment-specific. The frontend
    dev server's proxy target is hardcoded in web/vite.config.ts to
    127.0.0.1:9091, so api.addr should match that (or edit vite.config.ts).
#>
param(
    [Parameter(Mandatory = $true)]
    [string]$Config
)

$RepoRoot = Split-Path -Parent $PSScriptRoot

if (-not (Test-Path $Config)) {
    Write-Host "Config not found: $Config" -ForegroundColor Red
    Write-Host "Pass a real one: .\scripts\dev.ps1 -Config path\to\config.yaml" -ForegroundColor Red
    Write-Host "Or for a zero-config demo instead: .\scripts\try-it-out.ps1" -ForegroundColor Yellow
    exit 1
}

Write-Host "==> Building backend" -ForegroundColor Cyan
Push-Location $RepoRoot
go build -o bin\bop-dev.exe .\cmd\bop
$buildExit = $LASTEXITCODE
Pop-Location
if ($buildExit -ne 0) {
    Write-Host "go build failed - see the error above." -ForegroundColor Red
    exit 1
}

# A built binary, not `go run` - go run's subprocess wrapping makes a
# clean Stop-Process kill unreliable (the real compiled binary can be
# left orphaned behind the wrapper). This backend doesn't hot-reload on
# Go code changes either way - re-run this script after a backend change.
Write-Host "==> Starting backend (bin\bop-dev.exe controller)" -ForegroundColor Cyan
$backend = Start-Process -FilePath "$RepoRoot\bin\bop-dev.exe" -ArgumentList "--config", "`"$Config`"", "controller" -WorkingDirectory $RepoRoot -PassThru -NoNewWindow
if (-not $backend -or -not $backend.Id) {
    Write-Host "Backend failed to start." -ForegroundColor Red
    exit 1
}

# Not -FilePath "npm" directly: on Windows, `npm` resolves to npm.ps1/
# npm.cmd, not a real .exe, and Start-Process launching those directly
# doesn't reliably return a trackable, waitable process (observed: it
# hands back a PID that's already gone by the time Wait-Process runs).
# Routing through cmd.exe /c gives Start-Process a real, stable process to
# track - the same reasoning as PATHEXT resolution in an ordinary shell.
Write-Host "==> Starting frontend dev server (npm run dev)" -ForegroundColor Cyan
$frontend = Start-Process -FilePath "cmd.exe" -ArgumentList "/c", "npm run dev" -WorkingDirectory "$RepoRoot\web" -PassThru -NoNewWindow
if (-not $frontend -or -not $frontend.Id) {
    Write-Host "Frontend dev server failed to start." -ForegroundColor Red
    taskkill /T /F /PID $backend.Id 2>$null | Out-Null
    exit 1
}

Write-Host ""
Write-Host "Frontend (hot reload): http://localhost:5173/" -ForegroundColor Green
Write-Host "Backend/API directly:  whatever api.addr says in $Config" -ForegroundColor Green
Write-Host "Press Ctrl+C to stop both."
Write-Host ""

try {
    Wait-Process -Id $backend.Id, $frontend.Id
} finally {
    Write-Host "`n==> Stopping..." -ForegroundColor Cyan
    # taskkill /T, not Stop-Process: the frontend's tracked PID is cmd.exe,
    # and Stop-Process only kills that one process, leaving npm's actual
    # node/vite child running orphaned. /T kills the whole process tree.
    if ($backend -and $backend.Id) { taskkill /T /F /PID $backend.Id 2>$null | Out-Null }
    if ($frontend -and $frontend.Id) { taskkill /T /F /PID $frontend.Id 2>$null | Out-Null }
}
