# Signs one file with signtool, using a code-signing certificate from the
# Windows certificate store. The private key never leaves the store (or the
# hardware token), so nothing secret is kept in the repository.
#
#   $env:CYSTA_SIGN_THUMBPRINT = '<SHA-1 thumbprint of the certificate>'
#   $env:CYSTA_TIMESTAMP_URL   = 'http://timestamp.digicert.com'  # optional; your CA's RFC 3161 server
#   ./scripts/sign.ps1 build/bin/cysta-dyska.exe
#
# Also called by makensis (!finalize / !uninstfinalize) for the installer and
# the uninstaller when scripts/release.ps1 builds with signing enabled.
param([Parameter(Mandatory)][string]$File)
$ErrorActionPreference = 'Stop'

$thumb = $env:CYSTA_SIGN_THUMBPRINT
if (-not $thumb) { throw 'CYSTA_SIGN_THUMBPRINT is not set; nothing to sign with.' }
$ts = if ($env:CYSTA_TIMESTAMP_URL) { $env:CYSTA_TIMESTAMP_URL } else { 'http://timestamp.digicert.com' }

$signtool = (Get-Command signtool.exe -ErrorAction SilentlyContinue).Source
if (-not $signtool) {
    $signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" -ErrorAction SilentlyContinue |
        Sort-Object FullName -Descending | Select-Object -First 1 -ExpandProperty FullName
}
if (-not $signtool) { throw 'signtool.exe not found; install the Windows SDK signing tools.' }

& $signtool sign /sha1 $thumb /fd SHA256 /tr $ts /td SHA256 /d ([char]0x010C + 'ysta Dyska') $File
if ($LASTEXITCODE -ne 0) { throw "signtool failed for $File (exit $LASTEXITCODE)" }
& $signtool verify /pa $File
if ($LASTEXITCODE -ne 0) { throw "signature check failed for $File" }
