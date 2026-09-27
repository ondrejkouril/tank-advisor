# Payloads

A release build puts here what TankAdvisor.exe carries and writes out at start
(docs/spec-desktop.md section 9): wotctx.exe, wotctx-launcher.exe, the Claude
Desktop bundle and the client mod. `make installer` fills it; git keeps only
this file. A build with nothing here writes nothing out.
