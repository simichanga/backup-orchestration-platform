<#
.SYNOPSIS
    Spins up a real, throwaway backup target and runs a real bop controller
    against it, so you can see BOP actually do something without hand-writing
    any config yourself. See ../TESTING.md for what this does and why.

.PARAMETER Cleanup
    Removes the Docker container this script creates. Run this when you're
    done - the controller itself already stopped when you pressed Ctrl+C.
#>
param(
    [switch]$Cleanup
)

# Deliberately not $ErrorActionPreference = 'Stop': PowerShell 5.1 wraps a
# native command's stderr output as a terminating NativeCommandError under
# that setting, even on a harmless message and even on exit code 0 (e.g.
# "docker rm" on a container that doesn't exist yet). Real failures are
# instead checked explicitly via $LASTEXITCODE below.
$RepoRoot = Split-Path -Parent $PSScriptRoot
$Demo = Join-Path $env:TEMP 'bop-try-it-out'
$ContainerName = 'bop-demo-ssh'

if ($Cleanup) {
    docker rm -f $ContainerName | Out-Null
    Remove-Item -Recurse -Force $Demo -ErrorAction SilentlyContinue
    Write-Host "Cleaned up - the demo container and its temp files are gone."
    exit 0
}

# Well-known fallback locations for tools that are on PATH in a normal
# interactive session but can go missing from PATH when this script runs
# through a non-interactive parent (a background job, a scheduled task, a
# `make` target - PATH resolution for these has been observed to differ
# from an interactive shell even on the same machine, same user). Checked
# only if Get-Command can't find the tool on PATH first.
$FallbackToolPaths = @{
    'ssh-keygen'  = "$env:WINDIR\System32\OpenSSH\ssh-keygen.exe"
    'ssh-keyscan' = "$env:WINDIR\System32\OpenSSH\ssh-keyscan.exe"
}

function Require-Command($name, $hint) {
    if (Get-Command $name -ErrorAction SilentlyContinue) { return }

    $fallback = $FallbackToolPaths[$name]
    if ($fallback -and (Test-Path $fallback)) {
        $env:PATH = "$(Split-Path $fallback);$env:PATH"
        return
    }

    Write-Host "Missing '$name'. $hint" -ForegroundColor Red
    exit 1
}

# Out-File -Encoding utf8 writes a UTF-8 byte-order-mark on Windows
# PowerShell 5.1 (only PowerShell 6+ has utf8NoBOM). That BOM becomes part
# of a token/password file's content as far as bop is concerned - it reads
# the file bytes as-is, so a token that looks correct in a text editor
# silently fails to match what a client actually sends. Write these with
# .NET's UTF8Encoding($false) instead, which omits the BOM.
function Write-NoBom($Path, [string]$Content) {
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $utf8NoBom)
}

Require-Command go "Install Go from https://go.dev/dl/, then reopen your terminal."
Require-Command docker "Install and start Docker Desktop: https://www.docker.com/products/docker-desktop/"
Require-Command restic "Install restic and put restic.exe on your PATH: https://restic.net/"
Require-Command ssh-keygen "Turn on the 'OpenSSH Client' optional feature in Windows Settings."

# BOP's SSH plugin executor always connects on port 22 (see
# internal/sshexec) - the demo container has to bind exactly there, so
# nothing else can already be listening on it.
$portInUse = Get-NetTCPConnection -LocalPort 22 -State Listen -ErrorAction SilentlyContinue
if ($portInUse -and -not (docker ps --filter "name=$ContainerName" --format '{{.Names}}')) {
    Write-Host "Port 22 is already in use by something else on this machine (not this script)." -ForegroundColor Red
    Write-Host "Stop whatever that is (e.g. an existing SSH server) and try again." -ForegroundColor Red
    exit 1
}

Write-Host "==> Building bop.exe" -ForegroundColor Cyan
Push-Location $RepoRoot
go build -o bin\bop.exe .\cmd\bop
$buildExit = $LASTEXITCODE
Pop-Location
if ($buildExit -ne 0) {
    Write-Host "go build failed - see the error above." -ForegroundColor Red
    exit 1
}

Write-Host "==> Setting up a throwaway demo backup target" -ForegroundColor Cyan
Remove-Item -Recurse -Force $Demo -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path "$Demo\data", "$Demo\repo", "$Demo\tmp" -Force | Out-Null
Write-NoBom "$Demo\data\hello.txt" "hello from BOP, generated $(Get-Date)`n"
Write-NoBom "$Demo\restic-password.txt" "demo-restic-password"
Write-NoBom "$Demo\read-token.txt" "demo-read-token"
Write-NoBom "$Demo\write-token.txt" "demo-write-token"

ssh-keygen -t ed25519 -N '""' -f "$Demo\id_ed25519" -q | Out-Null

docker rm -f $ContainerName | Out-Null
$sshdScript = @'
apk add --no-cache openssh tar >/dev/null &&
adduser -D bopuser && passwd -u bopuser &&
mkdir -p /home/bopuser/.ssh && cp /tmp/authorized_keys /home/bopuser/.ssh/authorized_keys &&
chown -R bopuser:bopuser /home/bopuser/.ssh && chmod 700 /home/bopuser/.ssh && chmod 600 /home/bopuser/.ssh/authorized_keys &&
ssh-keygen -A &&
exec /usr/sbin/sshd -D -e
'@
docker run -d --name $ContainerName -p 127.0.0.1:22:22 `
    -v "${Demo}\id_ed25519.pub:/tmp/authorized_keys:ro" `
    -v "${Demo}\data:/home/bopuser/data:ro" `
    alpine:3.20 sh -c $sshdScript | Out-Null
if ($LASTEXITCODE -ne 0) {
    Write-Host "docker run failed - is Docker Desktop running?" -ForegroundColor Red
    exit 1
}

Write-Host "==> Waiting for the demo SSH target to come up" -ForegroundColor Cyan
Start-Sleep -Seconds 3
# Not ssh-keyscan: Windows' bundled ssh-keyscan.exe only offers a KEX
# algorithm (sntrup761x25519-sha512@openssh.com) this container's sshd
# doesn't support, so it silently returns nothing and known_hosts ends up
# empty - bop then fails every job with "knownhosts: key is unknown". We
# already control this container directly, so just read its host keys
# straight from the filesystem instead of negotiating a connection for
# them. All three types (ssh-keygen -A generated rsa/ecdsa/ed25519) go in,
# not just one - bop's SSH client picks whichever algorithm it negotiates,
# and a known_hosts entry for the wrong algorithm reads as "key mismatch"
# just like having no entry at all reads as "key is unknown".
$hostKeys = docker exec $ContainerName sh -c 'cat /etc/ssh/ssh_host_*_key.pub'
$knownHosts = ($hostKeys | ForEach-Object { "127.0.0.1 $_" }) -join "`n"
Write-NoBom "$Demo\known_hosts" "$knownHosts`n"

# YAML gets forward-slash paths - bop runs them through Go's path handling,
# which is fine with either separator, and this avoids backslash-escaping
# headaches inside the YAML strings below.
$DemoPath = $Demo -replace '\\', '/'

$inventoryYaml = @"
servers:
  demo-host:
    host: 127.0.0.1
    ssh_user: bopuser
    ssh_key: $DemoPath/id_ed25519
    plugins:
      filesystem:
        config:
          paths:
            - /home/bopuser/data
    retention:
      daily: 7
"@
Write-NoBom "$Demo\inventory.yaml" $inventoryYaml

$configYaml = @"
inventory: $DemoPath/inventory.yaml
storage:
  provider: restic
  restic:
    repository: $DemoPath/repo
    password_file: $DemoPath/restic-password.txt
metadata:
  driver: sqlite
  dsn: $DemoPath/metadata.db
controller:
  temp_dir: $DemoPath/tmp
ssh:
  known_hosts_file: $DemoPath/known_hosts
api:
  enabled: true
  addr: "127.0.0.1:9091"
  tokens_file: $DemoPath/read-token.txt
  write_tokens_file: $DemoPath/write-token.txt
metrics:
  port: 19093
"@
Write-NoBom "$Demo\config.yaml" $configYaml

Write-Host "==> Creating the restic repository" -ForegroundColor Cyan
$env:RESTIC_PASSWORD_FILE = "$Demo\restic-password.txt"
restic -r "$Demo\repo" init | Out-Null

Write-Host ""
Write-Host "==> Starting bop controller" -ForegroundColor Green
Write-Host "    Web UI:      http://127.0.0.1:9091/"
Write-Host "    Read token:  demo-read-token   (view only)"
Write-Host "    Write token: demo-write-token  (view, and trigger backups)"
Write-Host ""
Write-Host "    Press Ctrl+C to stop the controller when you're done."
Write-Host "    Then run: .\scripts\try-it-out.ps1 -Cleanup"
Write-Host ""

Start-Sleep -Seconds 1
Start-Process "http://127.0.0.1:9091/"
& "$RepoRoot\bin\bop.exe" --config "$Demo\config.yaml" controller
