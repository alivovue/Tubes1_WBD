#requires -Version 5.1
param([switch]$SkipBuild, [switch]$Check, [switch]$Stop)
& (Join-Path $PSScriptRoot "src\tools\launchers\run-dev.ps1") @PSBoundParameters
exit $LASTEXITCODE
