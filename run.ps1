#requires -Version 5.1
param([switch]$Admin, [switch]$Worker)
& (Join-Path $PSScriptRoot "src\tools\launchers\run.ps1") @PSBoundParameters
exit $LASTEXITCODE
