!include "MUI2.nsh"

Name "Never Sleep"
OutFile "/home/sandbox/neversleep/dist/Never Sleep Setup 1.3.0.exe"
Unicode true
InstallDir "$LOCALAPPDATA\Programs\Never Sleep"
RequestExecutionLevel user

!define MUI_ICON "/home/sandbox/neversleep/assets/icon.ico"
!define MUI_UNICON "/home/sandbox/neversleep/assets/icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\Never Sleep.exe"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath "$INSTDIR"
  File /r "/home/sandbox/neversleep/dist/win-unpacked/*.*"
  CreateShortcut "$DESKTOP\Never Sleep.lnk" "$INSTDIR\Never Sleep.exe"
  CreateDirectory "$SMPROGRAMS\Never Sleep"
  CreateShortcut "$SMPROGRAMS\Never Sleep\Never Sleep.lnk" "$INSTDIR\Never Sleep.exe"
  CreateShortcut "$SMPROGRAMS\Never Sleep\Uninstall Never Sleep.lnk" "$INSTDIR\Uninstall Never Sleep.exe"
  WriteUninstaller "$INSTDIR\Uninstall Never Sleep.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep" "DisplayName" "Never Sleep"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep" "DisplayIcon" "$INSTDIR\Never Sleep.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep" "UninstallString" "$INSTDIR\Uninstall Never Sleep.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep" "DisplayVersion" "1.3.0"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep" "Publisher" "Ahmed Roza"
SectionEnd

Section "Uninstall"
  Delete "$DESKTOP\Never Sleep.lnk"
  RMDir /r "$SMPROGRAMS\Never Sleep"
  RMDir /r "$INSTDIR"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep"
SectionEnd
