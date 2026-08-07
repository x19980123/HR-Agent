@echo off
REM Start Python stdlib agent (no third-party deps required)
set PYTHONPATH=%~dp0..\python
python -m hr_agent.api.stdlib_server
