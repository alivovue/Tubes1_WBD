#requires -Version 5.1

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$previousDirectory = Get-Location
$previousEnvironment = @{}
$fixtureState = ''
$retainFixtures = $false
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
    if (-not $fixtureState -or $retainFixtures) { return }
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

function Invoke-TestSuite([string]$Name, [string]$Package) {
    $log = Join-Path $reportDirectory ($Name + '.log')
    $encoding = New-Object Text.UTF8Encoding -ArgumentList $false
    $writer = New-Object IO.StreamWriter -ArgumentList $log, $false, $encoding
    $preference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & $goCommand test -count=1 -timeout=5m -v $Package 2>&1 | ForEach-Object {
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

function Read-AdminPassword {
    while ($true) {
        $first = Read-Host 'Admin password (12-72 UTF-8 bytes)' -AsSecureString
        $second = Read-Host 'Confirm password' -AsSecureString
        $firstPointer = [IntPtr]::Zero
        $secondPointer = [IntPtr]::Zero
        try {
            $firstPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($first)
            $secondPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($second)
            $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($firstPointer)
            $confirmation = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($secondPointer)
            $bytes = [Text.Encoding]::UTF8.GetByteCount($plain)
            if ($plain -ceq $confirmation -and $bytes -ge 12 -and $bytes -le 72) { return $plain }
            Write-Host 'Passwords must match and contain 12-72 bytes. Try again.'
        } finally {
            if ($firstPointer -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($firstPointer) }
            if ($secondPointer -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($secondPointer) }
            $first.Dispose()
            $second.Dispose()
            $plain = $null
            $confirmation = $null
        }
    }
}

try {
    Set-Location -LiteralPath (Join-Path $PSScriptRoot 'src')
    Import-LaunchEnvironment (Join-Path (Get-Location).Path '.env')
    $command = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if (-not $command) { throw 'Install Go for Windows and reopen the terminal before starting Schakker.' }
    $goCommand = $command.Source

    $runUnitTests = Read-LaunchChoice 'Run unit tests before launching the website?' $true
    $runIntegrationTests = Read-LaunchChoice 'Run PostgreSQL integration tests before launching the website?' $true

    if ($env:SCHAKKER_STATE_DIR) {
        $statePath = $env:SCHAKKER_STATE_DIR
        if (-not [IO.Path]::IsPathRooted($statePath)) { $statePath = Join-Path (Get-Location).Path $statePath }
        $stateDirectory = [IO.Path]::GetFullPath($statePath)
    } else {
        $localData = [Environment]::GetFolderPath('LocalApplicationData')
        $hash = [Security.Cryptography.SHA256]::Create()
        try {
            $projectKey = ([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($PSScriptRoot.ToLowerInvariant())))).Replace('-', '').Substring(0, 16).ToLowerInvariant()
        } finally { $hash.Dispose() }
        $stateDirectory = Join-Path $localData (Join-Path 'Schakker' $projectKey)
    }
    [void][IO.Directory]::CreateDirectory($stateDirectory)
    $stateDirectory = (Get-Item -LiteralPath $stateDirectory).FullName
    Set-LaunchEnvironment 'SCHAKKER_STATE_DIR' $stateDirectory
    Write-Host "Schakker state: $stateDirectory"

    $reportDirectory = ''
    $failures = 0
    if ($runUnitTests -or $runIntegrationTests) {
        $reportDirectory = Join-Path (Get-Location).Path ('test\output\' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '.' + [Guid]::NewGuid().ToString('N').Substring(0, 6))
        [void][IO.Directory]::CreateDirectory($reportDirectory)
        [IO.File]::WriteAllText((Join-Path $reportDirectory 'summary.txt'), "Schakker automated tests`r`n")
        Write-Host "Test reports: $reportDirectory"
    }
    if ($runUnitTests -and -not (Invoke-TestSuite 'unit' './test/unit/...')) { $failures++ }
    if ($runIntegrationTests) {
        $fixtureState = New-FixtureDirectory
        Set-LaunchEnvironment 'SCHAKKER_TEST_STATE' $fixtureState
        try {
            if (-not (Invoke-TestSuite 'integration' './test/integration/...')) { $failures++ }
        } finally { Set-LaunchEnvironment 'SCHAKKER_TEST_STATE' $null }
    }
    if ($failures -gt 0) { throw "Automated tests failed. The website was not started. Reports: $reportDirectory" }

    $useMockWebsite = Read-LaunchChoice 'Keep and use the mock website database for the website run?' $false
    $useMockManaged = Read-LaunchChoice 'Keep and use the mock PostgreSQL managed database for the website run?' $false
    $useMockMariaDB = Read-LaunchChoice 'Keep and use the mock MariaDB database for the website run?' $false
    Set-LaunchEnvironment 'SCHAKKER_USE_MOCK_WEBSITE' $(if ($useMockWebsite) { 'yes' } else { 'no' })
    Set-LaunchEnvironment 'SCHAKKER_USE_MOCK_MANAGED' $(if ($useMockManaged) { 'yes' } else { 'no' })
    Set-LaunchEnvironment 'SCHAKKER_USE_MOCK_MARIADB' $(if ($useMockMariaDB) { 'yes' } else { 'no' })
    if ($useMockWebsite -or $useMockManaged -or $useMockMariaDB) {
        $kept = Join-Path $stateDirectory 'kept-fixtures'
        if (-not $fixtureState -and (Test-Path -LiteralPath $kept -PathType Leaf)) {
            $previous = [IO.File]::ReadAllText($kept).Trim()
            if ($previous -and (Test-FixtureDirectory $previous)) { $fixtureState = $previous }
        }
        if (-not $fixtureState) { $fixtureState = New-FixtureDirectory }
        $retainFixtures = $true
        [IO.File]::WriteAllText($kept, $fixtureState + "`r`n")
        Set-LaunchEnvironment 'SCHAKKER_FIXTURE_STATE' $fixtureState
        Write-Host "Kept mock database files: $fixtureState"
    } else {
        Remove-TemporaryFixtures
        $fixtureState = ''
        Set-LaunchEnvironment 'SCHAKKER_FIXTURE_STATE' $null
    }

    $binary = Join-Path $stateDirectory 'server.exe'
    & $goCommand build -o $binary .
    if ($LASTEXITCODE -ne 0) { throw 'The website could not be built.' }
    & $binary
    $launchExitCode = $LASTEXITCODE
    if ($launchExitCode -eq 3) {
        if ([Console]::IsInputRedirected) { throw 'First startup needs a terminal or SCHAKKER_ADMIN_USERNAME and SCHAKKER_ADMIN_PASSWORD.' }
        Write-Host 'First-time admin setup. Your password is stored as a hash in the website database.'
        if ($useMockWebsite) { Write-Host 'The mock.writer and mock.reader accounts will use the password you choose here.' }
        $username = Read-Host 'Admin username [admin]'
        if (-not $username) { $username = 'admin' }
        $display = Read-Host 'Display name [Platform Admin]'
        if (-not $display) { $display = 'Platform Admin' }
        Set-LaunchEnvironment 'SCHAKKER_ADMIN_USERNAME' $username
        Set-LaunchEnvironment 'SCHAKKER_ADMIN_NAME' $display
        Set-LaunchEnvironment 'SCHAKKER_ADMIN_PASSWORD' (Read-AdminPassword)
        & $binary
        $launchExitCode = $LASTEXITCODE
    }
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    if ($launchExitCode -eq 0) { $launchExitCode = 1 }
} finally {
    try { Remove-TemporaryFixtures } catch { Write-Warning "Temporary database cleanup did not complete: $($_.Exception.Message)" }
    foreach ($name in $previousEnvironment.Keys) {
        Set-LaunchEnvironment $name $previousEnvironment[$name]
    }
    Set-Location -LiteralPath $previousDirectory.Path
}
exit $launchExitCode
