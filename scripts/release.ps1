# Builds the release: the app and its per-user NSIS installer in build\bin.
#
#   ./scripts/release.ps1           # unsigned (SmartScreen will warn users)
#   ./scripts/release.ps1 -Sign     # signs app, uninstaller and installer; see scripts/sign.ps1
#
# Needs Wails (matching go.mod) and NSIS (winget install NSIS.NSIS).
param([switch]$Sign)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

if (-not (Get-Command makensis.exe -ErrorAction SilentlyContinue)) {
    $nsis = "${env:ProgramFiles(x86)}\NSIS"
    if (-not (Test-Path "$nsis\makensis.exe")) { throw 'makensis.exe not found; install NSIS (winget install NSIS.NSIS).' }
    $env:PATH += ";$nsis"
}
if ($Sign -and -not $env:CYSTA_SIGN_THUMBPRINT) { throw '-Sign needs CYSTA_SIGN_THUMBPRINT; see scripts/sign.ps1.' }

# Builds the app and an installer, and writes wails_tools.nsh with the
# product info from wails.json.
wails build -clean -nsis
if ($LASTEXITCODE -ne 0) { throw 'wails build failed' }

$exe = Join-Path $root 'build\bin\cysta-dyska.exe'
$installer = Join-Path $root 'build\bin\cysta-dyska-amd64-installer.exe'

if ($Sign) {
    # Sign the app first, then package that signed exe again; makensis signs
    # the uninstaller and the installer through scripts/sign.ps1.
    & "$PSScriptRoot\sign.ps1" $exe
    Push-Location 'build\windows\installer'
    try {
        makensis "-DARG_WAILS_AMD64_BINARY=$exe" "-DCYSTA_SIGN=$PSScriptRoot\sign.ps1" project.nsi
        if ($LASTEXITCODE -ne 0) { throw 'makensis failed' }
    } finally { Pop-Location }
}

# The generated tools file and the downloaded WebView2 bootstrapper are build
# output, not sources.
git checkout -- build/windows/installer/wails_tools.nsh 2>$null

Get-Item $exe, $installer | ForEach-Object {
    $sig = (Get-AuthenticodeSignature $_.FullName).Status
    '{0,-36} {1,8:N1} MB  signature: {2}' -f $_.Name, ($_.Length / 1MB), $sig
}
