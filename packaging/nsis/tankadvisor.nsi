; Tank Advisor's installer (docs/spec-desktop.md section 4): per user, with no
; administrator rights. It copies one executable and asks a question or two;
; everything else is TankAdvisor.exe's own work (--install-payloads,
; --uninstall, --quit), which lives in Go and is tested there.
;
; make installer builds it:
;   makensis -DVERSION=1.2.3 -DAPP=<TankAdvisor.exe> -DWEBVIEW2=<bootstrapper> -DOUTFILE=<setup.exe> tankadvisor.nsi

Unicode true
; LZMA makes the smallest installer; a minimal NSIS without its stub can pass
; -DCOMPRESSOR=zlib.
!ifndef COMPRESSOR
  !define COMPRESSOR "/SOLID lzma"
!endif
SetCompressor ${COMPRESSOR}
RequestExecutionLevel user

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
; VERSION_NUM is VERSION as plain numbers (1.2.3), which Windows' version
; resource requires.
!ifndef VERSION_NUM
  !define VERSION_NUM "0.0.0"
!endif
!ifndef OUTFILE
  !define OUTFILE "TankAdvisor-setup.exe"
!endif
!ifndef APP
  !error "APP: the TankAdvisor.exe to install"
!endif
!ifndef WEBVIEW2
  !error "WEBVIEW2: Microsoft's WebView2 bootstrapper, MicrosoftEdgeWebview2Setup.exe"
!endif

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\TankAdvisor"
; The WebView2 runtime's id in EdgeUpdate.
!define WEBVIEW2_ID "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "Tank Advisor"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Tank Advisor"
BrandingText "Tank Advisor ${VERSION}"

!include "MUI2.nsh"
!include "LogicLib.nsh"

!define MUI_ICON "icon.ico"
!define MUI_UNICON "icon.ico"
!define MUI_LICENSEPAGE_TEXT_TOP "About Tank Advisor, your data, and the licence."
!define MUI_LICENSEPAGE_BUTTON "Install"
!define MUI_FINISHPAGE_RUN "$INSTDIR\TankAdvisor.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start Tank Advisor and set it up"

!insertmacro MUI_PAGE_LICENSE "notices.txt"
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

VIProductVersion "${VERSION_NUM}.0"
VIAddVersionKey "ProductName" "Tank Advisor"
VIAddVersionKey "FileDescription" "Tank Advisor installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "(c) 2026 ondrejkouril. Not affiliated with or endorsed by Wargaming."

Section "Install"
  ; A running Tank Advisor is asked to quit, so its file can be replaced.
  ${If} ${FileExists} "$INSTDIR\TankAdvisor.exe"
    ExecWait '"$INSTDIR\TankAdvisor.exe" --quit'
    Sleep 2000
  ${EndIf}

  SetOutPath "$INSTDIR"
  File "/oname=TankAdvisor.exe" "${APP}"
  File "icon.ico"

  Call EnsureWebView2

  ; The app writes out wotctx, the Claude Desktop bundle and the mod it
  ; carries, puts this folder on the user's PATH and records it for the
  ; Claude Desktop launcher.
  DetailPrint "Setting up Tank Advisor's files"
  ExecWait '"$INSTDIR\TankAdvisor.exe" --install-payloads' $0
  ${If} $0 != 0
    MessageBox MB_ICONEXCLAMATION|MB_OK "Tank Advisor could not set up all its files. Start it once; it tries again at every start." /SD IDOK
  ${EndIf}

  CreateShortCut "$SMPROGRAMS\Tank Advisor.lnk" "$INSTDIR\TankAdvisor.exe" "" "$INSTDIR\icon.ico"

  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "Tank Advisor"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "ondrejkouril"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\icon.ico"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "https://github.com/ondrejkouril/tank-advisor"
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
SectionEnd

; WebView2 draws the app's window. Windows 11 has it; an older Windows 10 may
; not, and then Microsoft's bootstrapper installs it.
Function EnsureWebView2
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_ID}" "pv"
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_ID}" "pv"
  ${EndIf}
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    DetailPrint "Installing Microsoft Edge WebView2"
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File "/oname=MicrosoftEdgeWebview2Setup.exe" "${WEBVIEW2}"
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install'
    SetOutPath "$INSTDIR"
  ${EndIf}
FunctionEnd

Section "Uninstall"
  ExecWait '"$INSTDIR\TankAdvisor.exe" --quit'
  Sleep 2000

  ; The data is kept unless the player says otherwise: its history cannot be
  ; fetched again (docs/spec.md section 6.1).
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "Also delete your World of Tanks data from this computer?$\r$\n$\r$\nThat is the synced history, the client mod's file and your Wargaming login. The history cannot be fetched again. Your advice settings are kept either way." /SD IDNO IDYES deleteData
    ExecWait '"$INSTDIR\TankAdvisor.exe" --uninstall' $0
    Goto cleaned
  deleteData:
    ExecWait '"$INSTDIR\TankAdvisor.exe" --uninstall --delete-data' $0
  cleaned:
  ${If} $0 != 0
    MessageBox MB_ICONEXCLAMATION|MB_OK "Some things could not be removed; if World of Tanks is running, close it and uninstall again. Details are in $TEMP\tankadvisor-uninstall.log." /SD IDOK
  ${EndIf}

  ; Only what the installer and the app put here: the folder may hold
  ; something else if it was chosen by hand.
  Delete "$INSTDIR\TankAdvisor.exe"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\wotctx.exe"
  Delete "$INSTDIR\wotctx.old.exe"
  Delete "$INSTDIR\wotctx-*.mcpb"
  Delete "$INSTDIR\ondrejkouril.wotctx_*.wotmod"
  Delete "$INSTDIR\*.new"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\Tank Advisor.lnk"
  DeleteRegKey HKCU "${UNINST_KEY}"

  MessageBox MB_ICONINFORMATION|MB_OK "Tank Advisor is removed. Its Claude Desktop extension stays until you remove it there: Settings, then Extensions." /SD IDOK
SectionEnd
