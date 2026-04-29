# httpssh-relay smoke test
# Run from the repository root: pwsh -File relay/scripts/smoke.ps1
#
# What it does:
#   1. Builds the relay binary
#   2. Starts it in the background with a known LAN bearer
#   3. Hits /api/health, POST /api/sessions, GET /api/sessions, DELETE
#   4. Reports pass/fail per check
#   5. Stops the relay
#
# Requirements: Go 1.22+ on PATH, PowerShell 7.

$ErrorActionPreference = "Stop"

$Bearer = "smoke-bearer-32-chars-or-more-12345"
$Listen = "127.0.0.1:18822"
$BaseUrl = "http://$Listen"

Push-Location (Join-Path $PSScriptRoot "..")
try {
  Write-Host "==> go mod tidy"
  go mod tidy

  Write-Host "==> go build"
  go build -o "httpssh-relay-smoke.exe" "./cmd/httpssh-relay"

  Write-Host "==> launching relay on $Listen"
  $proc = Start-Process -FilePath "./httpssh-relay-smoke.exe" `
    -ArgumentList "--listen", $Listen, "--bearer", $Bearer, "--log-level", "info" `
    -PassThru -RedirectStandardOutput "smoke-stdout.log" -RedirectStandardError "smoke-stderr.log"

  Start-Sleep -Seconds 1

  $headers = @{ "Authorization" = "Bearer $Bearer" }
  $jsonHeaders = @{ "Authorization" = "Bearer $Bearer"; "Content-Type" = "application/json" }
  $passed = 0
  $failed = 0

  function Check($name, $block) {
    try {
      & $block
      Write-Host "PASS: $name" -ForegroundColor Green
      $script:passed++
    } catch {
      Write-Host "FAIL: $name -- $($_.Exception.Message)" -ForegroundColor Red
      $script:failed++
    }
  }

  Check "GET /api/health returns ok" {
    $r = Invoke-RestMethod -Uri "$BaseUrl/api/health" -Headers $headers
    if ($r.status -ne "ok") { throw "status was '$($r.status)'" }
  }

  Check "GET /api/health denies missing bearer" {
    try {
      Invoke-RestMethod -Uri "$BaseUrl/api/health" -ErrorAction Stop
      throw "expected 401"
    } catch {
      if ($_.Exception.Response.StatusCode.value__ -ne 401) { throw }
    }
  }

  Check "GET /api/sessions empty list" {
    $r = Invoke-RestMethod -Uri "$BaseUrl/api/sessions" -Headers $headers
    if ($r.sessions -and $r.sessions.Count -gt 0) { throw "expected empty initial list" }
  }

  $sessId = $null
  Check "POST /api/sessions creates pwsh" {
    $body = @{ shell = "pwsh"; cols = 80; rows = 24 } | ConvertTo-Json
    $r = Invoke-RestMethod -Uri "$BaseUrl/api/sessions" -Method Post -Headers $jsonHeaders -Body $body
    if (-not $r.id) { throw "no session id returned" }
    $script:sessId = $r.id
  }

  Check "DELETE /api/sessions/{id} removes the session" {
    if (-not $sessId) { throw "no session id" }
    Invoke-RestMethod -Uri "$BaseUrl/api/sessions/$sessId" -Method Delete -Headers $headers | Out-Null
  }

  Write-Host ""
  Write-Host "Summary: $passed passed, $failed failed"

  if ($failed -gt 0) { exit 1 }
}
finally {
  if ($proc) {
    Write-Host "==> stopping relay (pid=$($proc.Id))"
    Stop-Process -Id $proc.Id -ErrorAction SilentlyContinue
  }
  Pop-Location
}
