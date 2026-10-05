param([ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version = "0.3.2")
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
& (Join-Path $PSScriptRoot "set-version.ps1") -Version $Version
$goCacheRoot = Join-Path $projectRoot ".cache\gopath"
$env:GOPATH = $goCacheRoot
$env:GOMODCACHE = Join-Path $projectRoot ".cache\gomod"
$env:GOCACHE = Join-Path $projectRoot ".cache\gobuild"
$env:CARGO_HOME = Join-Path $projectRoot ".cache\cargo"
$binDir = Join-Path $projectRoot "bin"
$distDir = Join-Path $projectRoot "dist"
$portableDir = Join-Path $distDir "TDL-Media-windows-x64-$Version"
$distFull = [IO.Path]::GetFullPath($distDir).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
$portableFull = [IO.Path]::GetFullPath($portableDir)
if (-not $portableFull.StartsWith($distFull, [StringComparison]::OrdinalIgnoreCase)) {
  throw "Portable output escaped the dist directory: $portableFull"
}
if (Test-Path -LiteralPath $portableFull) {
  Remove-Item -LiteralPath $portableFull -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $binDir,$distDir,$portableDir | Out-Null

Push-Location $projectRoot
try {
  go vet ./...
  if ($LASTEXITCODE -ne 0) { throw "Go vet failed" }
  go test ./...
  if ($LASTEXITCODE -ne 0) { throw "Go tests failed" }
  go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $binDir "tdl-media.exe") ./cmd/tdl-media
  if ($LASTEXITCODE -ne 0) { throw "CLI build failed" }
  Push-Location (Join-Path $projectRoot "gui")
  try {
    npm ci --cache .cache\npm
    if ($LASTEXITCODE -ne 0) { throw "npm dependency installation failed" }
    npm test
    if ($LASTEXITCODE -ne 0) { throw "Frontend tests failed" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
    npm run tauri build -- --no-bundle
    if ($LASTEXITCODE -ne 0) { throw "GUI build failed" }
  } finally { Pop-Location }
  $guiExe = Join-Path $projectRoot "gui\src-tauri\target\release\tdl-media-gui.exe"
  $peBytes = [IO.File]::ReadAllBytes($guiExe)
  $peOffset = [BitConverter]::ToInt32($peBytes, 0x3c)
  $subsystem = [BitConverter]::ToUInt16($peBytes, $peOffset + 24 + 68)
  if ($subsystem -ne 2) { throw "GUI executable must use the Windows GUI subsystem (2), got $subsystem" }
  Copy-Item -Force (Join-Path $binDir "tdl-media.exe") $portableDir
  Copy-Item -Force (Join-Path $projectRoot "gui\src-tauri\target\release\tdl-media-gui.exe") $portableDir
  Copy-Item -Force (Join-Path $projectRoot "README.md") $portableDir
  Copy-Item -Force (Join-Path $projectRoot "LICENSE") $portableDir
  $zipPath = "$portableDir.zip"
  if (Test-Path -LiteralPath $zipPath) { Remove-Item -LiteralPath $zipPath }
  Compress-Archive -Path (Join-Path $portableDir "*") -DestinationPath $zipPath
  Write-Host "Portable package: $zipPath"
} finally { Pop-Location }
