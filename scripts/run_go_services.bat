@echo off
cd /d %~dp0..\services
if not exist bin\api.exe (
  set GOPROXY=https://goproxy.cn,direct
  go build -o bin\api.exe .\cmd\api
  go build -o bin\mailer.exe .\cmd\mailer
  go build -o bin\ingress.exe .\cmd\ingress
)
start "hr-api" bin\api.exe
start "hr-mailer" bin\mailer.exe
start "hr-ingress" bin\ingress.exe
echo Go services started in separate windows.
