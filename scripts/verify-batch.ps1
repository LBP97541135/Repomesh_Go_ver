param(
    [ValidateSet("B00", "B01", "B02", "B03")]
    [string]$Batch = "B01",
    [string]$PostgresBin,
    [string]$EvidenceDirectory
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$binarySuffix = if ($IsWindows) { ".exe" } else { "" }
$pathComparison = if ($IsWindows) { [StringComparison]::OrdinalIgnoreCase } else { [StringComparison]::Ordinal }
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
if (-not $EvidenceDirectory) {
    if ($Batch -eq "B03") {
        $EvidenceDirectory = Join-Path $repoRoot ("docs/development/2026-09-12-b03-01/backend/verification-" + [DateTime]::UtcNow.ToString("yyyyMMdd-HHmmss"))
    }
    else {
        $EvidenceDirectory = Join-Path $repoRoot ("docs/development/verification-" + $Batch.ToLower() + "-" + [DateTime]::UtcNow.ToString("yyyyMMdd-HHmmss"))
    }
}
$evidenceRoot = [System.IO.Path]::GetFullPath($EvidenceDirectory)
if (Test-Path -LiteralPath $evidenceRoot) {
    throw "Evidence directory already exists. Choose a new directory."
}
$null = New-Item -ItemType Directory -Path $evidenceRoot
$tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$runRoot = Join-Path $tempRoot ("repomesh-verify-" + [Guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $runRoot
$records = [System.Collections.Generic.List[object]]::new()
$secrets = [System.Collections.Generic.List[string]]::new()
$savedEnvironment = @{}
$dbStarted = $false
$webProcess = $null
$client = $null
$failure = $null
$passed = $false
$cleanupPassed = $true

function Set-BatchEnvironment([string]$Name, [AllowNull()][string]$Value) {
    if (-not $savedEnvironment.ContainsKey($Name)) {
        $savedEnvironment[$Name] = [Environment]::GetEnvironmentVariable($Name, "Process")
    }
    [Environment]::SetEnvironmentVariable($Name, $Value, "Process")
}

function Protect-BatchOutput([string]$Value) {
    foreach ($secret in $secrets) {
        if ($secret) { $Value = $Value.Replace($secret, "[REDACTED]") }
    }
    return $Value
}

function New-BatchProcessInfo([string]$File, [string[]]$Arguments) {
    $info = [System.Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $File
    $info.WorkingDirectory = $repoRoot
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
    return $info
}

function Invoke-BatchCommand {
    param(
        [string]$Name,
        [string]$File,
        [string[]]$Arguments = @(),
        [int[]]$ExpectedExit = @(0),
        [int]$TimeoutSeconds = 240,
        [switch]$NoCapture,
        [string]$ExpectedOutput
    )
    $info = New-BatchProcessInfo $File $Arguments
    $info.RedirectStandardOutput = -not $NoCapture
    $info.RedirectStandardError = -not $NoCapture
    if (-not $NoCapture) {
        $info.StandardOutputEncoding = [System.Text.Encoding]::UTF8
        $info.StandardErrorEncoding = [System.Text.Encoding]::UTF8
    }
    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $info
    $watch = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        if (-not $process.Start()) { throw "Could not start $Name" }
        if (-not $NoCapture) {
            $stdoutTask = $process.StandardOutput.ReadToEndAsync()
            $stderrTask = $process.StandardError.ReadToEndAsync()
        }
        if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
            $process.Kill($true)
            $process.WaitForExit()
            throw "$Name exceeded its timeout."
        }
        $stdout = ""
        $stderr = ""
        if (-not $NoCapture) {
            if (-not [System.Threading.Tasks.Task]::WaitAll([System.Threading.Tasks.Task[]]@($stdoutTask, $stderrTask), 5000)) { throw "$Name output did not close after process exit." }
            $stdout = Protect-BatchOutput $stdoutTask.GetAwaiter().GetResult()
            $stderr = Protect-BatchOutput $stderrTask.GetAwaiter().GetResult()
        }
        $record = [ordered]@{
            name = $Name
            command = @($File) + $Arguments
            exitCode = $process.ExitCode
            expectedExit = $ExpectedExit
            seconds = [Math]::Round($watch.Elapsed.TotalSeconds, 3)
            stdout = $stdout
            stderr = $stderr
        }
        $records.Add($record)
        if ($ExpectedExit -notcontains $process.ExitCode) {
            throw "$Name exited $($process.ExitCode), expected $ExpectedExit. $stderr"
        }
        if ($ExpectedOutput -and ($stdout + $stderr) -notmatch $ExpectedOutput) {
            throw "$Name did not produce its expected diagnostic: $ExpectedOutput"
        }
        if ($ExpectedOutput) { $record["expectedOutput"] = $ExpectedOutput }
        Write-Host "PASS $Name"
        return $record
    }
    finally { $process.Dispose() }
}

function Get-BatchPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try { return $listener.LocalEndpoint.Port }
    finally { $listener.Stop() }
}

try {
    $configCount = 0
    if ($env:GIT_CONFIG_COUNT) { $configCount = [int]$env:GIT_CONFIG_COUNT }
    Set-BatchEnvironment "GIT_CONFIG_COUNT" ([string]($configCount + 1))
    Set-BatchEnvironment ("GIT_CONFIG_KEY_" + $configCount) "safe.directory"
    Set-BatchEnvironment ("GIT_CONFIG_VALUE_" + $configCount) $repoRoot.Replace("\", "/")
    Set-BatchEnvironment "NO_COLOR" "1"
    Set-BatchEnvironment "REPOMESH_AUTH_CONFIG" $null
    Set-BatchEnvironment "GOCACHE" (Join-Path $runRoot "go-cache")
    $migrationTarget = @(Get-ChildItem -LiteralPath (Join-Path $repoRoot "internal/database/migrations") -Filter "*.sql").Count
    $go = (Get-Command go -CommandType Application | Select-Object -First 1).Source
    $node = (Get-Command node -CommandType Application | Select-Object -First 1).Source
    if ($IsWindows) {
        $npm = Join-Path (Split-Path $node) "node_modules/npm/bin/npm-cli.js"
        if (-not (Test-Path -LiteralPath $npm)) { throw "Cannot find npm-cli.js beside node." }
    }
    else {
        $npm = (Get-Command npm -CommandType Application | Select-Object -First 1).Source
        $npmTarget = (Get-Item -LiteralPath $npm).ResolveLinkTarget($true)
        if ($npmTarget) { $npm = $npmTarget.FullName }
    }
    $null = Invoke-BatchCommand "go-version" $go @("version")
    $null = Invoke-BatchCommand "node-version" $node @("--version")
    $null = Invoke-BatchCommand "npm-version" $node @($npm, "--version")

    if ($Batch -in @("B01", "B02", "B03")) {
        if (-not $PostgresBin) { throw "$Batch requires -PostgresBin pointing to PostgreSQL binaries." }
        $PostgresBin = (Resolve-Path -LiteralPath $PostgresBin).Path
        foreach ($tool in @("initdb", "pg_ctl", "postgres", "psql")) {
            $name = $tool + $binarySuffix
            if (-not (Test-Path -LiteralPath (Join-Path $PostgresBin $name))) { throw "Missing $name" }
        }
        $null = Invoke-BatchCommand "postgres-version" (Join-Path $PostgresBin ("postgres" + $binarySuffix)) @("--version")
        $password = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
        $secrets.Add($password)
        $passwordFile = Join-Path $runRoot "password.txt"
        [System.IO.File]::WriteAllText($passwordFile, $password, [System.Text.UTF8Encoding]::new($false))
        $data = Join-Path $runRoot "data"
        $dbPort = Get-BatchPort
        $null = Invoke-BatchCommand "initdb" (Join-Path $PostgresBin ("initdb" + $binarySuffix)) @("-D", $data, "-U", "repomesh_test", "--pwfile=$passwordFile", "--auth-host=scram-sha-256", "--auth-local=scram-sha-256", "--encoding=UTF8", "--locale=C")
        Remove-Item -LiteralPath $passwordFile
        Add-Content -LiteralPath (Join-Path $data "postgresql.conf") -Value "unix_socket_directories = ''"
        $pgOptions = "-h 127.0.0.1 -p $dbPort -c fsync=on"
        $dbStarted = $true
        $null = Invoke-BatchCommand "postgres-start" (Join-Path $PostgresBin ("pg_ctl" + $binarySuffix)) @("-D", $data, "-l", (Join-Path $runRoot "postgres.log"), "-o", $pgOptions, "-w", "-t", "30", "start") -TimeoutSeconds 40 -NoCapture
        $url = "postgres://repomesh_test:${password}@127.0.0.1:${dbPort}/postgres?sslmode=disable"
        $secrets.Insert(0, $url)
        Set-BatchEnvironment "REPOMESH_TEST_DATABASE_URL" $url
        Set-BatchEnvironment "REPOMESH_DATABASE_URL" $url
        Set-BatchEnvironment "PGPASSWORD" $password
    }
    else {
        Set-BatchEnvironment "REPOMESH_TEST_DATABASE_URL" $null
        Set-BatchEnvironment "REPOMESH_DATABASE_URL" $null
    }

    $null = Invoke-BatchCommand "go-build" $go @("build", "./...")
    $goTests = Invoke-BatchCommand "go-test" $go @("test", "-json", "-count=1", "./...")
    if ($Batch -in @("B01", "B02", "B03")) {
        $events = @($goTests.stdout -split "`n" | Where-Object { $_.Trim() } | ForEach-Object { $_ | ConvertFrom-Json })
        $postgresEvents = @($events | Where-Object { $_.PSObject.Properties.Name -contains "Test" -and $_.Test -like "TestPostgres*" })
        $postgresPasses = @($postgresEvents | Where-Object { $_.Action -eq "pass" })
        $postgresSkips = @($postgresEvents | Where-Object { $_.Action -eq "skip" })
        if ($postgresPasses.Count -eq 0 -or $postgresSkips.Count -ne 0) {
            throw "$Batch needs executed PostgreSQL tests. Missing or skipped tests are not a pass."
        }
        $records.Add([ordered]@{name="postgres-test-gate"; passedTests=$postgresPasses.Count; skippedTests=$postgresSkips.Count})
    }
    $null = Invoke-BatchCommand "go-vet" $go @("vet", "./...")
    if ($Batch -ne "B03") {
        $null = Invoke-BatchCommand "npm-ci" $node @($npm, "--prefix", "web", "ci")
        $null = Invoke-BatchCommand "npm-typecheck" $node @($npm, "--prefix", "web", "run", "typecheck")
        $null = Invoke-BatchCommand "npm-build" $node @($npm, "--prefix", "web", "run", "build")
    }
    if ($Batch -eq "B02") {
        $null = Invoke-BatchCommand "browser-api-tests" $node @("--experimental-strip-types", "web/src/api.test.mjs")
        $null = Invoke-BatchCommand "authentication-race" $go @("test", "-race", "-count=1", "./internal/access", "./internal/secrets", "./internal/web", "./internal/github")
    }
    if ($Batch -eq "B03") {
        $null = Invoke-BatchCommand "project-race" $go @("test", "-race", "-count=1", "./internal/access", "./internal/secrets", "./internal/projects", "./internal/web", "./internal/github", "./internal/database") -TimeoutSeconds 600
    }
    $webExe = Join-Path $runRoot ("repomesh-web" + $binarySuffix)
    $null = Invoke-BatchCommand "web-binary" $go @("build", "-o", $webExe, "./cmd/repomesh-web")
    foreach ($entry in @("coordinator", "host-executor")) {
        $binary = Join-Path $runRoot ("repomesh-" + $entry + $binarySuffix)
        $null = Invoke-BatchCommand "$entry-binary" $go @("build", "-o", $binary, "./cmd/repomesh-$entry")
        $null = Invoke-BatchCommand "$entry-version" $binary @("--version")
        $expected = Invoke-BatchCommand "$entry-unimplemented" $binary @() -ExpectedExit 1
        if (($expected.stdout + $expected.stderr) -notmatch "not implemented|authentication is not configured") { throw "$entry did not report its real implementation state." }
    }
    $null = Invoke-BatchCommand "web-version" $webExe @("--version")

    if ($Batch -in @("B01", "B02", "B03")) {
        $null = Invoke-BatchCommand "db-check-empty" $webExe @("db", "check") -ExpectedExit 1 -ExpectedOutput "schema status=missing current=0 target=$migrationTarget pending=$migrationTarget"
        $null = Invoke-BatchCommand "db-migrate" $webExe @("db", "migrate") -ExpectedOutput "schema status=current current=$migrationTarget target=$migrationTarget pending=0"
        $null = Invoke-BatchCommand "db-check" $webExe @("db", "check") -ExpectedOutput "schema status=current current=$migrationTarget target=$migrationTarget pending=0"
        $null = Invoke-BatchCommand "db-migrate-repeat" $webExe @("db", "migrate") -ExpectedOutput "schema status=current current=$migrationTarget target=$migrationTarget pending=0"
        $null = Invoke-BatchCommand "postgres-restart-stop" (Join-Path $PostgresBin ("pg_ctl" + $binarySuffix)) @("-D", $data, "-m", "fast", "-w", "-t", "30", "stop") -TimeoutSeconds 40
        $null = Invoke-BatchCommand "postgres-restart-start" (Join-Path $PostgresBin ("pg_ctl" + $binarySuffix)) @("-D", $data, "-l", (Join-Path $runRoot "postgres.log"), "-o", $pgOptions, "-w", "-t", "30", "start") -TimeoutSeconds 40 -NoCapture
        $null = Invoke-BatchCommand "db-check-after-server-restart" $webExe @("db", "check") -ExpectedOutput "schema status=current current=$migrationTarget target=$migrationTarget pending=0"
        $psqlArgs = @("-h", "127.0.0.1", "-p", [string]$dbPort, "-U", "repomesh_test", "-d", "postgres", "-X", "-v", "ON_ERROR_STOP=1", "-At")
        $ledger = Invoke-BatchCommand "db-ledger" (Join-Path $PostgresBin ("psql" + $binarySuffix)) ($psqlArgs + @("-c", "SELECT version, name, octet_length(checksum) FROM public.repomesh_schema_migrations ORDER BY version"))
        $ledgerRows = @($ledger.stdout.Trim() -split "`n")
        if ($ledgerRows.Count -ne $migrationTarget) { throw "Unexpected migration ledger count." }
        for ($index=0; $index -lt $ledgerRows.Count; $index++) {
            if ($ledgerRows[$index].Trim() -notmatch ("^" + ($index+1) + "\|[^|]+\|32$")) { throw "Unexpected migration ledger row." }
        }
        $null = Invoke-BatchCommand "db-tamper" (Join-Path $PostgresBin ("psql" + $binarySuffix)) ($psqlArgs + @("-c", "UPDATE public.repomesh_schema_migrations SET name = 'tampered' WHERE version = 1"))
        $null = Invoke-BatchCommand "db-check-drift" $webExe @("db", "check") -ExpectedExit 1 -ExpectedOutput "migration history does not match this binary: name or checksum differs at version 1"
        $null = Invoke-BatchCommand "db-migrate-drift" $webExe @("db", "migrate") -ExpectedExit 1 -ExpectedOutput "migration history does not match this binary: name or checksum differs at version 1"
        $null = Invoke-BatchCommand "db-invalid-command" $webExe @("db", "reset") -ExpectedExit 2 -ExpectedOutput "expected db check or db migrate"
        Set-BatchEnvironment "REPOMESH_DATABASE_URL" $null
        $null = Invoke-BatchCommand "db-missing-url" $webExe @("db", "check") -ExpectedExit 2 -ExpectedOutput "invalid database configuration: a connection string is required"
    }

    $webPort = Get-BatchPort
    $webProcess = [System.Diagnostics.Process]::new()
    $webInfo = New-BatchProcessInfo $webExe @("--addr", "127.0.0.1:$webPort", "--assets", (Join-Path $repoRoot "web/dist"))
    $webInfo.RedirectStandardOutput = $true
    $webInfo.RedirectStandardError = $true
    $webProcess.StartInfo = $webInfo
    if (-not $webProcess.Start()) { throw "Web binary failed to start." }
    $webOut = $webProcess.StandardOutput.ReadToEndAsync()
    $webErr = $webProcess.StandardError.ReadToEndAsync()
    $client = [System.Net.Http.HttpClient]::new()
    $client.Timeout = [TimeSpan]::FromSeconds(2)
    $listening = $false
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        try {
            $response = $client.GetAsync("http://127.0.0.1:$webPort/healthz").GetAwaiter().GetResult()
            $listening = $true
            $response.Dispose()
            break
        }
        catch {
            if ($webProcess.HasExited) { throw "Web exited before serving HTTP." }
            Start-Sleep -Milliseconds 100
        }
    }
    if (-not $listening) { throw "Web did not listen before deadline." }
    foreach ($probe in @(@("/healthz", 200), @("/readyz", 503), @("/api/projects", 503), @("/api/session", 503), @("/login", 200), @("/", 200))) {
        $response = $client.GetAsync("http://127.0.0.1:$webPort" + $probe[0]).GetAwaiter().GetResult()
        try {
            $status = [int]$response.StatusCode
            $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            if ($status -ne $probe[1]) { throw "Unexpected HTTP status for $($probe[0]): $status" }
            if ($probe[0] -eq "/healthz" -and ($body | ConvertFrom-Json).businessReady -ne $false) { throw "Database foundation must not claim business readiness." }
            if ($probe[0] -eq "/") {
                $expectedHTML = [System.IO.File]::ReadAllText((Join-Path $repoRoot "web/dist/index.html"))
                if ($body -cne $expectedHTML -or $response.Content.Headers.ContentType.MediaType -ne "text/html") { throw "Web did not serve the built index.html." }
                $digest = [Convert]::ToHexString([System.Security.Cryptography.SHA256]::HashData([System.Text.Encoding]::UTF8.GetBytes($body))).ToLowerInvariant()
                $records.Add([ordered]@{name="http-/"; status=$status; expected=$probe[1]; contentType="text/html"; matchesBuiltIndex=$true; sha256=$digest})
            }
            else {
                $records.Add([ordered]@{name="http-" + $probe[0]; status=$status; expected=$probe[1]; body=$body})
            }
            Write-Host "PASS HTTP $($probe[0])"
        }
        finally { $response.Dispose() }
    }
    $passed = $true
}
catch {
    $failure = Protect-BatchOutput $_.Exception.Message
    Write-Warning $failure
}
finally {
    if ($client) { $client.Dispose() }
    if ($webProcess) {
        if (-not $webProcess.HasExited) { $webProcess.Kill($true); $webProcess.WaitForExit() }
        $webProcess.Dispose()
    }
    if ($dbStarted) {
        try {
            $status = Invoke-BatchCommand "postgres-status" (Join-Path $PostgresBin ("pg_ctl" + $binarySuffix)) @("-D", $data, "status") -ExpectedExit @(0, 3)
            if ($status.exitCode -eq 0) {
                $null = Invoke-BatchCommand "postgres-stop" (Join-Path $PostgresBin ("pg_ctl" + $binarySuffix)) @("-D", $data, "-m", "fast", "-w", "-t", "30", "stop") -TimeoutSeconds 40
            }
            $databaseLog = Join-Path $runRoot "postgres.log"
            if (Test-Path -LiteralPath $databaseLog) {
                Protect-BatchOutput (Get-Content -LiteralPath $databaseLog -Raw) | Set-Content -LiteralPath (Join-Path $evidenceRoot "postgres-output.txt") -Encoding utf8
            }
        }
        catch { $cleanupPassed = $false; $failure = $failure + " PostgreSQL cleanup failed. Inspect retained directory $runRoot. " + $_.Exception.Message }
    }
    foreach ($entry in $savedEnvironment.GetEnumerator()) { [Environment]::SetEnvironmentVariable($entry.Key, $entry.Value, "Process") }
    if ($cleanupPassed) {
        $resolvedRun = [System.IO.Path]::GetFullPath($runRoot)
        $expectedPrefix = [System.IO.Path]::Combine($tempRoot, "repomesh-verify-")
        $pathSeparators = [char[]]@([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
        $resolvedParent = (Split-Path $resolvedRun -Parent).TrimEnd($pathSeparators)
        $expectedParent = $tempRoot.TrimEnd($pathSeparators)
        if (-not $resolvedRun.StartsWith($expectedPrefix, $pathComparison) -or -not [string]::Equals($resolvedParent, $expectedParent, $pathComparison)) {
            $cleanupPassed = $false
            $failure = "Refusing cleanup outside the created temporary directory."
        }
        else {
            try { Remove-Item -LiteralPath $resolvedRun -Recurse -Force }
            catch { $cleanupPassed = $false; $failure = "Could not remove the owned temporary directory: " + $_.Exception.Message }
        }
    }
    $result = [ordered]@{
        batch = $Batch
        result = $(if ($passed -and $cleanupPassed) { if ($Batch -eq "B02") {"LOCAL_VERIFIED"} elseif ($Batch -eq "B03") {"BACKEND_LOCAL_VERIFIED"} else {"VERIFIED"} } else {"NOT_VERIFIED"})
        externalAcceptance = $(if ($Batch -eq "B02") {"NOT_RUN: real GitHub App and browser OAuth acceptance required"} elseif ($Batch -eq "B03") {"NOT_RUN: actual browser acceptance and release-wide verification are separate"} else {$null})
        recordedAt = [DateTime]::UtcNow.ToString("o")
        failure = $failure
        cleanupPassed = $cleanupPassed
        checks = @($records.ToArray())
    }
    $result | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $evidenceRoot "checks.json") -Encoding utf8
}
if (-not $passed -or -not $cleanupPassed) { throw "Batch verification failed. See $evidenceRoot/checks.json" }
Write-Host "Local checks passed for $Batch. Evidence: $evidenceRoot/checks.json"
