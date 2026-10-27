$ErrorActionPreference = 'Stop'

$repo = if ($env:CHISTORE_RELEASE_REPO) { $env:CHISTORE_RELEASE_REPO } else { 'name-cho/Chistore-go' }
$binDir = if ($env:CHISTORE_BIN_DIR) { $env:CHISTORE_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\chistore\bin' }

switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$asset = "chistore_windows_$arch.exe"
$base = "https://github.com/$repo/releases/latest/download"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

$tmp = Join-Path $binDir '.chistore.tmp'
$sums = Join-Path ([System.IO.Path]::GetTempPath()) 'chistore-sums.txt'

try {
    Write-Host "[i] Downloading $asset"
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $tmp

    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $sums
        $line = Get-Content $sums | Where-Object { ($_ -split '\s+')[1] -eq $asset } | Select-Object -First 1
        if ($line) {
            $expected = ($line -split '\s+')[0].ToLowerInvariant()
            $actual = (Get-FileHash -Algorithm SHA256 -Path $tmp).Hash.ToLowerInvariant()
            if ($actual -ne $expected) { throw "checksum mismatch for $asset" }
        }
    } catch {
        if ($_.Exception.Message -like 'checksum mismatch*') { throw }
        Write-Warning 'checksums.txt unavailable; skipping verification'
    }

    $target = Join-Path $binDir 'chistore.exe'
    Move-Item -Force -Path $tmp -Destination $target
    Write-Host "[+] Installed chistore to $target"
    Write-Host "[!] Add $binDir to PATH to run chistore from anywhere"
} finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $tmp, $sums
}
