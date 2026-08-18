Set-Location $PSScriptRoot\..\web\admin-ui
npm run build
Write-Host "Done. Set HR_ADMIN_VUE=1 and restart Go API to serve Vue admin."
