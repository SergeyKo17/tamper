# Drives the recorded demo: prints each command as if typed, runs it, holds the
# frame. Run it with a screen recorder capturing the terminal window.

# Every path below is relative to this script, so the caller's directory does
# not matter.
Set-Location $PSScriptRoot

function Show-Command([string]$cmd, [int]$pause = 2) {
    Write-Host -NoNewline "PS demo> " -ForegroundColor Green
    foreach ($ch in $cmd.ToCharArray()) {
        Write-Host -NoNewline $ch
        Start-Sleep -Milliseconds 20
    }
    Write-Host ""
    Invoke-Expression $cmd | Out-Host
    Start-Sleep -Seconds $pause
}

function Call([int]$port) {
    '''{"text":"hello"}'' | grpcurl -plaintext -d ''@'' 127.0.0.1:' + $port + ' demo.Echo/Say'
}

# A crashed run leaves the flipped line behind; start from a known state.
(Get-Content config\demo.yml) -replace 'type: abort', 'type: delay' | Set-Content config\demo.yml

Copy-Item config\demo.yml "$env:TEMP\demo.yml.bak" -Force

# Off camera, because both are slow: recreate the stack so the proxy reads this
# config, and pay its first dial to the target. Reflection matches no rule, so
# the log frame stays clean.
docker compose up -d --force-recreate 2>&1 | Out-Null
grpcurl -plaintext 127.0.0.1:9090 list 2>$null | Out-Null

Write-Host "Stack is up. Start the recorder, then press Enter." -ForegroundColor Yellow
Read-Host | Out-Null

# The real prompt carries a home directory; the frames use their own.
Clear-Host
Write-Host -NoNewline "PS demo> " -ForegroundColor Green
Write-Host ".\run"

Show-Command 'docker compose up -d' 2
Show-Command (Call 50051) 1
Show-Command (Call 9090) 1
Show-Command '(Get-Content config\demo.yml) -replace ''type: delay'', ''type: abort'' | Set-Content config\demo.yml' 1
Show-Command 'docker compose kill -s HUP tamper' 1
Show-Command (Call 9090) 2
Show-Command 'docker compose logs tamper | Select-String "fault applied"' 3

Copy-Item "$env:TEMP\demo.yml.bak" config\demo.yml -Force
