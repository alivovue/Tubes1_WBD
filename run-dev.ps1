#requires -Version 5.1
param([switch]$SkipBuild, [switch]$Check, [switch]$Stop)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$devPreviousDirectory = Get-Location
$devPreviousGoWork = $env:GOWORK
$devExitCode = 1
try {
    $devGo = Get-Command go.exe -ErrorAction Stop
    $devState = Join-Path $PSScriptRoot 'src\.schakker\dev'
    $devBinary = Join-Path $devState 'bin\dev-server.exe'
    if ($Stop) {
        if (-not (Test-Path -LiteralPath $devBinary)) { throw 'Local development has not been started yet.' }
        & $devBinary -root $PSScriptRoot -stop
        $devExitCode = $LASTEXITCODE
    } else {
        if (-not $SkipBuild) {
            $devNpm = Get-Command npm.cmd -ErrorAction Stop
            Set-Location -LiteralPath (Join-Path $PSScriptRoot 'src')
            if (-not (Test-Path -LiteralPath 'node_modules\.bin\vite.cmd')) {
                & $devNpm.Source ci --no-audit --no-fund
                if ($LASTEXITCODE -ne 0) { throw 'npm dependencies could not be installed.' }
            }
            & $devNpm.Source run build
            if ($LASTEXITCODE -ne 0) { throw 'The frontend build failed.' }
            [void][IO.Directory]::CreateDirectory((Join-Path $devState 'bin'))
            $env:GOWORK = 'off'
            foreach ($devBuild in @(
                @{ Module = 'src'; Package = '.'; Output = 'user-server.exe' },
                @{ Module = 'src-admin'; Package = '.'; Output = 'admin-server.exe' },
                @{ Module = 'src'; Package = './cmd/worker'; Output = 'migration-worker.exe' },
                @{ Module = 'tests'; Package = './cmd/devserver'; Output = 'dev-server.exe' }
            )) {
                $devModuleDirectory = Join-Path (Join-Path $PSScriptRoot 'src') $devBuild.Module
                $devOutput = Join-Path (Join-Path $devState 'bin') $devBuild.Output
                & $devGo.Source -C $devModuleDirectory build -o $devOutput $devBuild.Package
                if ($LASTEXITCODE -ne 0) { throw "Go build failed: $($devBuild.Output)" }
            }
        }
        if (-not (Test-Path -LiteralPath $devBinary)) { throw 'Run without -SkipBuild the first time.' }
        $devArguments = @('-root', $PSScriptRoot)
        if ($Check) { $devArguments += '-check' }
        & $devBinary @devArguments
        $devExitCode = $LASTEXITCODE
    }
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
} finally {
    $env:GOWORK = $devPreviousGoWork
    Set-Location -LiteralPath $devPreviousDirectory.Path
}
exit $devExitCode
