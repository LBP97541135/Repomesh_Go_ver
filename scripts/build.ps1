param(
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]*$')]
    [string]$Version = 'dev'
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$releasePath = Join-Path $repoRoot "dist/repomesh-$Version"

function Invoke-Checked {
    param([string]$Program, [string[]]$Arguments)
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Program failed with exit code $LASTEXITCODE"
    }
}

Push-Location $repoRoot
try {
    # Never overwrite an older bundle or silently mix assets from two builds.
    if (Test-Path -LiteralPath $releasePath) {
        throw "Release directory already exists: $releasePath. Choose a new version."
    }
    Invoke-Checked npm @('--prefix', 'web', 'ci')
    Invoke-Checked npm @('--prefix', 'web', 'run', 'build')
    New-Item -ItemType Directory -Path "$releasePath/bin", "$releasePath/web", "$releasePath/configs" | Out-Null

    $targetOS = (& go env GOOS).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Could not determine GOOS' }
    $targetArch = (& go env GOARCH).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Could not determine GOARCH' }
    $suffix = if ($targetOS -eq 'windows') { '.exe' } else { '' }
    foreach ($name in @('repomesh-web', 'repomesh-coordinator', 'repomesh-host-executor')) {
        Invoke-Checked go @('build', '-buildvcs=false', '-trimpath', '-ldflags', "-X repomesh.local/repomesh/internal/buildinfo.Version=$Version", '-o', "$releasePath/bin/$name$suffix", "./cmd/$name")
    }
    Copy-Item -LiteralPath 'web/dist' -Destination "$releasePath/web/dist" -Recurse
    Copy-Item -LiteralPath 'configs/repomesh.env.example' -Destination "$releasePath/configs/repomesh.env.example"
    Copy-Item -LiteralPath 'configs/auth.example.json' -Destination "$releasePath/configs/auth.example.json"
    Copy-Item -LiteralPath 'web/package-lock.json' -Destination "$releasePath/web/package-lock.json"
    Copy-Item -LiteralPath 'README.md' -Destination "$releasePath/SOURCE-README.md"
    @"
RepoMesh $Version ($targetOS/$targetArch) - authentication and project management with database tools

Run from this bundle directory:
  ./bin/repomesh-web$suffix --assets ./web/dist
  ./bin/repomesh-coordinator$suffix --version
  ./bin/repomesh-host-executor$suffix --version

Set REPOMESH_DATABASE_URL before explicit database operations:
  ./bin/repomesh-web$suffix db check
  ./bin/repomesh-web$suffix db migrate --timeout 30s
Database commands do not require web assets or start HTTP.
Web without authentication configuration does not connect to a database.
Configured Web and coordinator require current migrations; they never migrate automatically.

For Linux authentication, copy configs/auth.example.json to your deployment directory.
Configure your GitHub App, exact HTTPS origin/callback and protected secret files.
Set REPOMESH_AUTH_CONFIG and REPOMESH_DATABASE_URL for both processes.
Run ./bin/repomesh-web$suffix --assets ./web/dist and ./bin/repomesh-coordinator$suffix.
Do not place secret material in this release directory.
Use direct TLS files or a controlled HTTPS reverse proxy; never weaken Secure cookies.

Web: http://127.0.0.1:8080 ; /healthz = process only ; /readyz = 503.
Coordinator exits 1 without auth configuration; configured it performs auth maintenance.
Host executor exits 1: not implemented.
Project management supports pending-configuration projects, listing, project metadata edits,
explicit repository additions, pinned configuration references and original-operation recovery.
Issue management, runtime execution, AgentTeams, Docker and Python integration remain unimplemented.
All three binaries and web assets belong to this one release bundle.
Local authentication and project management checks do not prove real GitHub or full business acceptance.
"@ | Set-Content -LiteralPath "$releasePath/README.txt" -Encoding utf8
    $artifactHashes = @{}
    Get-ChildItem -LiteralPath $releasePath -File -Recurse | ForEach-Object {
        $relative = [System.IO.Path]::GetRelativePath($releasePath, $_.FullName).Replace('\', '/')
        $artifactHashes[$relative] = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    [ordered]@{
        version = $Version
        stage = 'project-management-local'
        businessReady = $false
        target = "$targetOS/$targetArch"
        artifacts = $artifactHashes
    } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath "$releasePath/release.json" -Encoding utf8
    Write-Output "Release bundle: $releasePath"
} finally {
    Pop-Location
}
