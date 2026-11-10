#requires -Version 5.1
param([switch]$Admin)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$previousDirectory = Get-Location
$previousEnvironment = @{}
$fixtureState = ''
$stateDirectory = [IO.Path]::GetTempPath().TrimEnd([IO.Path]::DirectorySeparatorChar)
$launchExitCode = 0

function Set-LaunchEnvironment([string]$Name, [AllowNull()]$Value) {
    if (-not $previousEnvironment.ContainsKey($Name)) {
        $previousEnvironment[$Name] = [Environment]::GetEnvironmentVariable($Name, 'Process')
    }
    if ($null -eq $Value) {
        [Environment]::SetEnvironmentVariable($Name, [NullString]::Value, 'Process')
    } else {
        [Environment]::SetEnvironmentVariable($Name, $Value, 'Process')
    }
}

function Import-LaunchEnvironment([string]$Filename) {
    if (-not (Test-Path -LiteralPath $Filename -PathType Leaf)) { return }
    $lineNumber = 0
    foreach ($line in [IO.File]::ReadAllLines($Filename)) {
        $lineNumber++
        $text = $line.Trim()
        if ($text -eq '' -or $text.StartsWith('#')) { continue }
        if ($text -notmatch '^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
            throw "Invalid .env line ${lineNumber}: use NAME=value assignments."
        }
        $name, $value = $Matches[1], $Matches[2].Trim()
        if ($value -match '^("|'')(.*)\1\s*(?:#.*)?$') {
            $value = $Matches[2]
        } elseif ($value.StartsWith('"') -or $value.StartsWith("'")) {
            throw "Invalid .env quoting on line ${lineNumber}."
        } else {
            $value = ($value -replace '\s+#.*$', '').TrimEnd()
        }
        Set-LaunchEnvironment $name $value
    }
}

function Read-LaunchChoice([string]$Question, [bool]$Default) {
    if ([Console]::IsInputRedirected) { return $Default }
    $suffix = '[y/N]'
    if ($Default) { $suffix = '[Y/n]' }
    while ($true) {
        $answer = (Read-Host "$Question $suffix").Trim().ToLowerInvariant()
        switch ($answer) {
            '' { return $Default }
            'y' { return $true }
            'yes' { return $true }
            'n' { return $false }
            'no' { return $false }
            default { Write-Host 'Please answer yes or no.' }
        }
    }
}

function Get-PostgresDirectory {
    $directories = @()
    if ($env:SCHAKKER_POSTGRES_BIN) { $directories += $env:SCHAKKER_POSTGRES_BIN }
    $command = Get-Command pg_ctl.exe -CommandType Application -ErrorAction SilentlyContinue
    if ($command) { $directories += Split-Path -Parent $command.Source }
    foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)})) {
        if (-not $root) { continue }
        $installed = Get-ChildItem -Path (Join-Path $root 'PostgreSQL\*\bin') -Directory -ErrorAction SilentlyContinue
        $directories += @($installed | Sort-Object FullName -Descending | ForEach-Object { $_.FullName })
    }
    foreach ($directory in $directories) {
        if (Test-Path -LiteralPath (Join-Path $directory 'pg_ctl.exe') -PathType Leaf) { return $directory }
    }
    return ''
}

function New-FixtureDirectory {
    $path = Join-Path $stateDirectory ('fixtures.' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
    [void][IO.Directory]::CreateDirectory($path)
    return $path
}

function Test-FixtureDirectory([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) { return $false }
    $item = Get-Item -LiteralPath $Path -Force
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { return $false }
    $fullPath = [IO.Path]::GetFullPath($Path)
    return [string]::Equals([IO.Path]::GetDirectoryName($fullPath), $stateDirectory, [StringComparison]::OrdinalIgnoreCase) -and
        ([IO.Path]::GetFileName($fullPath) -match '^fixtures\.[A-Za-z0-9]+$') -and
        (Test-Path -LiteralPath (Join-Path $fullPath '.schakker-fixtures') -PathType Leaf)
}

function Remove-TemporaryFixtures {
    if (-not $fixtureState) { return }
    if (-not (Test-FixtureDirectory $fixtureState)) {
        if ((Test-Path -LiteralPath $fixtureState -PathType Container) -and
            -not (Get-ChildItem -LiteralPath $fixtureState -Force | Select-Object -First 1)) {
            Remove-Item -LiteralPath $fixtureState
        }
        return
    }
    $postgresPID = Join-Path $fixtureState 'postgres\postmaster.pid'
    if (Test-Path -LiteralPath $postgresPID) {
        $directory = Get-PostgresDirectory
        if ($directory) {
            $preference = $ErrorActionPreference
            try {
                $ErrorActionPreference = 'Continue'
                & (Join-Path $directory 'pg_ctl.exe') -D (Join-Path $fixtureState 'postgres') -m fast -w -t 15 stop *> $null
            } finally { $ErrorActionPreference = $preference }
        }
    }
    if (-not (Test-Path -LiteralPath $postgresPID) -and
        -not (Test-Path -LiteralPath (Join-Path $fixtureState 'mariadb\server.pid'))) {
        Remove-Item -LiteralPath $fixtureState -Recurse -Force
    }
}

function Invoke-TestSuite([string]$Name, [string]$Package, [string]$Directory = $testDirectory) {
    $log = Join-Path $reportDirectory ($Name + '.log')
    $timeout = if ($Name -eq 'integration') { '10m' } else { '5m' }
    $encoding = New-Object Text.UTF8Encoding -ArgumentList $false
    $writer = New-Object IO.StreamWriter -ArgumentList $log, $false, $encoding
    $preference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & $goCommand -C $Directory test -count=1 "-timeout=$timeout" -v $Package 2>&1 | ForEach-Object {
            $line = $_.ToString()
            Write-Host $line
            $writer.WriteLine($line)
        }
        $status = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $preference
        $writer.Dispose()
    }
    $result = 'PASS'
    if ($status -ne 0) { $result = 'FAIL' }
    [IO.File]::AppendAllText((Join-Path $reportDirectory 'summary.txt'), "$result $Name`r`n")
    return $status -eq 0
}

try {
    $sourceDirectory = Join-Path $PSScriptRoot 'src'
    $testDirectory = Join-Path $sourceDirectory 'tests'
    $applicationDirectory = if ($Admin) { Join-Path $sourceDirectory 'src-admin' } else { Join-Path $sourceDirectory 'src' }
    Set-Location -LiteralPath $applicationDirectory
    Import-LaunchEnvironment (Join-Path $applicationDirectory '.env')
    if (-not $Admin -and $env:SCHAKKER_STATE_DIR -and -not [IO.Path]::IsPathRooted($env:SCHAKKER_STATE_DIR)) {
        Set-LaunchEnvironment 'SCHAKKER_STATE_DIR' ([IO.Path]::GetFullPath((Join-Path $applicationDirectory $env:SCHAKKER_STATE_DIR)))
    }
    $command = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if (-not $command) { throw 'Install Go for Windows and reopen the terminal before starting Schakker.' }
    $goCommand = $command.Source
    $npm = Get-Command npm.cmd -CommandType Application -ErrorAction SilentlyContinue
    if (-not $npm) { throw 'Install Node.js 22.12+ (with npm) before starting Schakker.' }
    $frontendWorkspace = if ($Admin) { 'schakker-admin-frontend' } else { 'schakker-frontend' }
    Push-Location -LiteralPath $sourceDirectory
    try {
        & $npm.Source ci "--workspace=$frontendWorkspace" '--workspace=@schakker/ui' --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed.' }
        & $npm.Source run build "--workspace=$frontendWorkspace"
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    } finally { Pop-Location }
    $runUnitTests = Read-LaunchChoice 'Run unit tests before launching the website?' $true
    $runIntegrationTests = $false
    if (-not $Admin) { $runIntegrationTests = Read-LaunchChoice 'Run PostgreSQL integration tests before launching the website?' $true }
    $reportDirectory = ''
    $failures = 0
    if ($runUnitTests -or $runIntegrationTests) {
        $reportMode = if ($Admin) { 'admin' } else { 'user' }
        $reportBase = Join-Path $testDirectory ('output\' + $reportMode)
        $reportDirectory = Join-Path $reportBase ([DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '.' + [Guid]::NewGuid().ToString('N').Substring(0, 6))
        [void][IO.Directory]::CreateDirectory($reportDirectory)
        [IO.File]::WriteAllText((Join-Path $reportDirectory 'summary.txt'), "Schakker automated tests`r`n")
        Write-Host "Test reports: $reportDirectory"
    }
    if ($runUnitTests) {
        Set-LaunchEnvironment 'GOWORK' 'off'
        if (-not (Invoke-TestSuite 'shared' './unit/shared/...')) { $failures++ }
        if ($Admin) {
            if (-not (Invoke-TestSuite 'unit' './unit/src-admin/...')) { $failures++ }
        } else {
            if (-not (Invoke-TestSuite 'unit' './unit/src/...')) { $failures++ }
        }
    }
    if ($runIntegrationTests) {
        Set-LaunchEnvironment 'GOWORK' 'off'
        $fixtureState = New-FixtureDirectory
        Set-LaunchEnvironment 'SCHAKKER_TEST_STATE' $fixtureState
        if (-not (Invoke-TestSuite 'integration' './integration/...')) { $failures++ }
        Remove-TemporaryFixtures
        $fixtureState = ''
        Set-LaunchEnvironment 'SCHAKKER_TEST_STATE' $null
    }
    if ($failures -gt 0) { throw "Automated tests failed. Reports: $reportDirectory" }
    $binaryDirectory = Join-Path $applicationDirectory 'bin'
    [void][IO.Directory]::CreateDirectory($binaryDirectory)
    $binaryName = if ($Admin) { 'admin-server.exe' } else { 'user-server.exe' }
    $binary = Join-Path $binaryDirectory $binaryName
    Set-LaunchEnvironment 'GOWORK' 'off'
    & $goCommand build -o $binary '.'
    if ($LASTEXITCODE -ne 0) { throw 'The website could not be built.' }
    & $binary
    $launchExitCode = $LASTEXITCODE
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    if ($launchExitCode -eq 0) { $launchExitCode = 1 }
} finally {
    try { Remove-TemporaryFixtures } catch { Write-Warning "Temporary test database cleanup did not complete: $($_.Exception.Message)" }
    foreach ($name in $previousEnvironment.Keys) { Set-LaunchEnvironment $name $previousEnvironment[$name] }
    Set-Location -LiteralPath $previousDirectory.Path
}
exit $launchExitCode
