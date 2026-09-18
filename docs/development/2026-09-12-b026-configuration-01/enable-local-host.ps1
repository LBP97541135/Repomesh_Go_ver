$ErrorActionPreference = 'Stop'

$hostsPath = Join-Path $env:SystemRoot 'System32\drivers\etc\hosts'
$hostname = 'repomesh.bohanxu.me'
$address = '127.0.0.1'

$existingAddresses = foreach ($line in [IO.File]::ReadAllLines($hostsPath)) {
    $active = ($line -split '#', 2)[0].Trim()
    if ($active -eq '') { continue }
    $fields = $active -split '\s+'
    if ($fields.Count -ge 2 -and $fields[1..($fields.Count - 1)] -contains $hostname) {
        $fields[0]
    }
}

if ($existingAddresses | Where-Object { $_ -ne $address }) {
    throw "Refusing to overwrite an existing mapping for $hostname."
}
if ($existingAddresses -contains $address) {
    Write-Host "$hostname already maps to $address. No change made."
    exit 0
}

$bytes = [IO.File]::ReadAllBytes($hostsPath)
$backupPath = Join-Path $env:TEMP ("hosts.repomesh-b026.{0}.bak" -f [guid]::NewGuid().ToString('N'))
$backup = [IO.File]::Open($backupPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
try {
    $backup.Write($bytes, 0, $bytes.Length)
} finally {
    $backup.Dispose()
}

$prefix = if ($bytes.Length -gt 0 -and $bytes[$bytes.Length - 1] -notin 10, 13) { "`r`n" } else { '' }
$addition = [Text.Encoding]::ASCII.GetBytes("${prefix}${address}`t${hostname}`r`n")
$stream = [IO.File]::Open($hostsPath, [IO.FileMode]::Append, [IO.FileAccess]::Write, [IO.FileShare]::Read)
try {
    $stream.Write($addition, 0, $addition.Length)
} finally {
    $stream.Dispose()
}

Write-Host "Added $address $hostname. Original bytes backed up to $backupPath"
