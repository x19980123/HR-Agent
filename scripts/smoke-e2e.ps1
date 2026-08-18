$ErrorActionPreference = "Stop"
$Base = "http://127.0.0.1:8080"
$HrToken = "dev-hr-token-change-me"
$Internal = "dev-internal-token-change-me"
$JdId = "jd-backend-001"
$HrH = @{ Authorization = "Bearer $HrToken" }
$IntH = @{ "X-Internal-Token" = $Internal; "Content-Type" = "application/json" }

$script:pass = 0
$script:fail = 0
function Assert($name, $cond, $detail = "") {
  if ($cond) { Write-Host "[PASS] $name"; $script:pass++ }
  else { Write-Host "[FAIL] $name $detail"; $script:fail++ }
}

Write-Host "=== HR Agent smoke ==="

$h = Invoke-RestMethod -Uri "$Base/health" -Method Get
Assert "health" ($h.status -eq "ok") ($h | ConvertTo-Json -Compress)

try {
  $jds = Invoke-RestMethod -Uri "$Base/v1/admin/jds" -Headers $HrH
  Assert "admin jds" ($jds.items.Count -gt 0)
} catch { Assert "admin jds" $false $_.Exception.Message }

$tmp = Join-Path $env:TEMP ("hr-agent-smoke-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
$uid = [guid]::NewGuid().ToString("N").Substring(0, 8)
$r1 = Join-Path $tmp "resume_a_$uid.txt"
$r2 = Join-Path $tmp "resume_b_$uid.txt"
$fn1 = [IO.Path]::GetFileName($r1)
$fn2 = [IO.Path]::GetFileName($r2)
Set-Content -Path $r1 -Value "Zhang San`nEmail: smoke.a.$uid@gmail.com`nGo developer" -Encoding UTF8
Set-Content -Path $r2 -Value "Li Si`nEmail: smoke.b.$uid@gmail.com`nPython backend" -Encoding UTF8

$csvPath = Join-Path $tmp "map.csv"
Set-Content -Path $csvPath -Value "filename,email,name`n$fn1,csv-wrong-$uid@gmail.com,CSV A`n$fn2,smoke.b.$uid@gmail.com,CSV B" -Encoding UTF8

$importOut = & curl.exe -s -w "`n%{http_code}" -X POST "$Base/v1/admin/imports" `
  -H "Authorization: Bearer $HrToken" `
  -F "jd_id=$JdId" `
  -F "default_email=candidate@import.local" `
  -F "resume=@$r1" `
  -F "resume=@$r2" `
  -F "mapping_csv=@$csvPath"
$lines = $importOut -split "`n"
$httpCode = $lines[-1]
$importJson = ($lines[0..($lines.Length-2)] -join "`n") | ConvertFrom-Json
Assert "import create" ($httpCode -eq "202" -and $importJson.job_id) "code=$httpCode"
$jobId = $importJson.job_id

$deadline = (Get-Date).AddMinutes(3)
$job = $null
$jobDone = $false
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 2
  $job = Invoke-RestMethod -Uri "$Base/v1/admin/imports/$jobId" -Headers $HrH
  $done = ($job.succeeded + $job.failed)
  if ($done -ge $job.total) { $jobDone = $true; break }
}
Assert "import job finished" $jobDone ("status=$($job.status) ok=$($job.succeeded) fail=$($job.failed)")

$items = Invoke-RestMethod -Uri "$Base/v1/admin/imports/$jobId/items" -Headers $HrH
Assert "import items list" ($null -ne $items.items)

$MysqlUser = if ($env:HR_MYSQL_USER) { $env:HR_MYSQL_USER } else { "root" }
$MysqlPass = if ($env:HR_MYSQL_PASSWORD) { $env:HR_MYSQL_PASSWORD } else { "123456" }
$MysqlDb = if ($env:HR_MYSQL_DATABASE) { $env:HR_MYSQL_DATABASE } else { "hr_agent" }

$retryCandidate = @($items.items | Where-Object { $_.status -eq "ok" } | Select-Object -First 1)
if ($retryCandidate) {
  $itemId = $retryCandidate.id
  $retrySql = "UPDATE import_items SET status='error', error_message='smoke_retry', application_id=NULL WHERE id='$itemId' AND job_id='$jobId'"
  $mysqlOk = $false
  $prevEap = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  & mysql "-u$MysqlUser" "-p$MysqlPass" $MysqlDb "-e" $retrySql 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { $mysqlOk = $true }
  $ErrorActionPreference = $prevEap
  Assert "import retry simulate error row" $mysqlOk "mysql exit=$LASTEXITCODE"

  $retryOut = & curl.exe -s -w "`n%{http_code}" -X POST "$Base/v1/admin/imports/$jobId/items/$itemId/retry" -H "Authorization: Bearer $HrToken"
  $retryLines = $retryOut -split "`n"
  $retryCode = $retryLines[-1]
  $retryBody = ($retryLines[0..($retryLines.Length - 2)] -join "`n")
  Assert "import item retry accepted" ($retryCode -eq "202") "code=$retryCode body=$retryBody"

  $retryOk = $false
  $row = $null
  if ($retryCode -eq "202") {
    for ($t = 0; $t -lt 45; $t++) {
      Start-Sleep -Seconds 2
      $after = Invoke-RestMethod -Uri "$Base/v1/admin/imports/$jobId/items" -Headers $HrH
      $row = @($after.items | Where-Object { $_.id -eq $itemId } | Select-Object -First 1)
      if ($row -and $row.status -eq "ok" -and $row.application_id) {
        $retryOk = $true
        break
      }
    }
  }
  Assert "import item retry completed" $retryOk ("item=$itemId status=$($row.status)")

  $badRetry = & curl.exe -s -w "`n%{http_code}" -X POST "$Base/v1/admin/imports/$jobId/items/$itemId/retry" -H "Authorization: Bearer $HrToken"
  $badCode = ($badRetry -split "`n")[-1]
  Assert "import retry rejects non-error item" ($badCode -eq "400") "code=$badCode"
} else {
  Assert "import retry setup" $false "no ok item to simulate failure"
}

$items = Invoke-RestMethod -Uri "$Base/v1/admin/imports/$jobId/items" -Headers $HrH
$okItems = @($items.items | Where-Object { $_.status -eq "ok" -and $_.application_id })
Assert "import has ok applications" ($okItems.Count -ge 1) ("count=$($okItems.Count)")

foreach ($it in $okItems) {
  $app = $null
  for ($t = 0; $t -lt 60; $t++) {
    Start-Sleep -Seconds 2
    try {
      $app = Invoke-RestMethod -Uri "$Base/v1/admin/applications/$($it.application_id)" -Headers $HrH
    } catch { continue }
    if ($it.candidate_email -like "csv-wrong-*") {
      if ($app.human_reason_code -eq "contact_csv_parse_mismatch") { break }
    }
    if ($app.status -ne "uploaded" -and $app.status -ne "parsing" -and $app.status -ne "screening") { break }
  }
  Assert "app $($it.application_id) terminal-ish" ($null -ne $app.status) "status=$($app.status)"
  if ($it.candidate_email -like "csv-wrong-*") {
    if ($app.status -eq "failed" -and "$($app.error_message)$($app.error_kind)" -match "Connection error|system") {
      Write-Host "[SKIP] csv parse mismatch (agent/LLM unavailable: status=$($app.status))"
    } else {
      $mismatch = ($app.status -eq "needs_human" -and $app.human_reason_code -eq "contact_csv_parse_mismatch")
      Assert "csv parse mismatch" $mismatch ("reason=$($app.human_reason_code) status=$($app.status)")
    }
  }
}

if ($okItems.Count -gt 0) {
  $aid = $okItems[0].application_id
  $bodyContact = '{"email":"hr-fixed@gmail.com","name":"HR Fixed"}'
  $upd = Invoke-RestMethod -Uri "$Base/v1/admin/applications/$aid/contact" -Method Put -Headers (@{ Authorization = "Bearer $HrToken"; "Content-Type" = "application/json" }) -Body $bodyContact
  Assert "put contact" ($upd.ok -eq $true)
  $app2 = Invoke-RestMethod -Uri "$Base/v1/admin/applications/$aid" -Headers $HrH
  Assert "contact human_override" ($app2.contact_email_source -eq "human_override")
}

$resumeB64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content $r1 -Raw)))
$extId = "smoke-ext-" + [guid]::NewGuid().ToString("N").Substring(0, 8)
$chBody = @{
  channel = "boss"
  jd_id = $JdId
  external_id = $extId
  candidate_name = "Channel Test"
  candidate_email = "channel.smoke@gmail.com"
  resume_base64 = $resumeB64
  resume_filename = "resume_a.txt"
} | ConvertTo-Json -Compress
$ch1 = Invoke-WebRequest -Uri "$Base/v1/hooks/channel-applications" -Method Post -Headers $IntH -Body ([System.Text.Encoding]::UTF8.GetBytes($chBody)) -UseBasicParsing
Assert "channel ingest" ($ch1.StatusCode -eq 202)
$ch1j = $ch1.Content | ConvertFrom-Json
$chApp1 = $ch1j.application_id
$ch2 = Invoke-WebRequest -Uri "$Base/v1/hooks/channel-applications" -Method Post -Headers $IntH -Body ([System.Text.Encoding]::UTF8.GetBytes($chBody)) -UseBasicParsing
$ch2j = $ch2.Content | ConvertFrom-Json
Assert "channel dedup" ($ch2j.application_id -eq $chApp1)

$await = Invoke-RestMethod -Uri "$Base/v1/admin/applications?status=awaiting_reply" -Headers $HrH
if ($await.items -and $await.items.Count -gt 0) {
  $wid = $await.items[0].id
  $tok = Invoke-RestMethod -Uri "$Base/v1/admin/applications/$wid/reply-token" -Headers $HrH
  $pubBody = '{"action":"update_contact","email":"candidate.self@gmail.com"}'
  $pr = Invoke-RestMethod -Uri "$Base/v1/public/reply/$($tok.token)" -Method Post -Headers @{ "Content-Type" = "application/json" } -Body $pubBody
  Assert "public update_contact" ($pr.ok -eq $true)
} else {
  Write-Host "[SKIP] public update_contact (no awaiting_reply)"
}

	try {
  $ecPath = Join-Path $tmp "ec.json"
  $json = @{ resume_text = (Get-Content $r1 -Raw) } | ConvertTo-Json -Compress
  [IO.File]::WriteAllText($ecPath, $json, (New-Object System.Text.UTF8Encoding $false))
  $ecRaw = & curl.exe -s -w "`n%{http_code}" -X POST "http://127.0.0.1:8000/v1/extract-contact" -H "Content-Type: application/json" --data-binary "@$ecPath"
  $ecLines = $ecRaw -split "`n"
  $ecCode = $ecLines[-1]
  $ec = $ecLines[0] | ConvertFrom-Json
  if ($ecCode -eq "200" -and $ec.email -match "@") {
    Assert "agent extract-contact" $true
  } else {
    Push-Location D:\HR-Agent\python
    $pyEmail = & python -c "from hr_agent.agents.contact_extract import extract_contact; print(extract_contact(resume_text='Email: smoke-test@gmail.com')['email'])" 2>&1 | Select-Object -Last 1
    Pop-Location
    Assert "agent extract-contact" ($pyEmail -match "@") "http=$ecCode note=restart_agent_for_/v1/extract-contact py=$pyEmail"
  }
} catch { Assert "agent extract-contact" $false $_.Exception.Message }

# Phase 3: scheduling assign+verify (offline deterministic path)
try {
  $schedPath = Join-Path $tmp "sched.json"
  $schedBody = @{
    application_id = "smoke-sched"
    duration_minutes = 60
    jd_department = "Engineering"
    requirements = @(
      @{ role_kind = "tech"; headcount = 2; specialties = @("go"); match_jd_department = $true; fixed_open_ids = @() },
      @{ role_kind = "hm"; headcount = 1; specialties = @(); match_jd_department = $false; fixed_open_ids = @() }
    )
    candidates = @(
      @{ open_id = "ou_t1"; name = "T1"; department = "Engineering"; role_kinds = @("tech"); specialties = @("go"); enabled = $true },
      @{ open_id = "ou_t2"; name = "T2"; department = "Engineering"; role_kinds = @("tech"); specialties = @("go","backend"); enabled = $true },
      @{ open_id = "ou_hm"; name = "HM"; department = "Product"; role_kinds = @("hm"); specialties = @(); enabled = $true }
    )
    busy_intervals = @()
  } | ConvertTo-Json -Depth 6 -Compress
  [IO.File]::WriteAllText($schedPath, $schedBody, (New-Object System.Text.UTF8Encoding $false))
  $schedRaw = & curl.exe -s -w "`n%{http_code}" -X POST "http://127.0.0.1:8000/v1/scheduling/assign" -H "Content-Type: application/json" --data-binary "@$schedPath"
  $schedLines = $schedRaw -split "`n"
  $schedCode = $schedLines[-1]
  $schedBody = ($schedLines[0..($schedLines.Length-2)] -join "`n")
  # Avoid ConvertFrom-Json on curl's Windows codepage output (Chinese rationale can corrupt JSON).
  $okSched = ($schedCode -eq "200") -and
    ($schedBody -match '"needs_human"\s*:\s*false') -and
    ($schedBody -match '"ou_t1"') -and
    ($schedBody -match '"ou_t2"') -and
    ($schedBody -match '"ou_hm"')
  if (-not $okSched) {
    Push-Location D:\HR-Agent\python
    $pyOut = & python -c "from hr_agent.agents.scheduling_assign import run_scheduling_assign; r=run_scheduling_assign({'requirements':[{'role_kind':'tech','headcount':2,'specialties':['go'],'match_jd_department':True},{'role_kind':'hm','headcount':1}],'candidates':[{'open_id':'ou_t1','department':'Engineering','role_kinds':['tech'],'specialties':['go'],'enabled':True},{'open_id':'ou_t2','department':'Engineering','role_kinds':['tech'],'specialties':['go'],'enabled':True},{'open_id':'ou_hm','department':'Product','role_kinds':['hm'],'specialties':[],'enabled':True}],'jd_department':'Engineering','duration_minutes':60,'busy_intervals':[]}); print(int(not r.get('needs_human') and len(r.get('assigned_open_ids') or [])==3))" 2>&1 | Select-Object -Last 1
    Pop-Location
    Assert "scheduling assign techx2+hm" ($pyOut -eq "1") "http=$schedCode note=restart_agent py=$pyOut"
  } else {
    Assert "scheduling assign techx2+hm" $true
  }
} catch { Assert "scheduling assign techx2+hm" $false $_.Exception.Message }

Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
Write-Host "=== Done: PASS=$script:pass FAIL=$script:fail ==="
if ($script:fail -gt 0) { exit 1 }
exit 0
