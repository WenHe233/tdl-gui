param(
  [Parameter(Mandatory)]
  [ValidatePattern('^\d+\.\d+\.\d+$')]
  [string]$Version
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$utf8NoBom = [Text.UTF8Encoding]::new($false)

function Set-VersionMatch {
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$Pattern,
    [Parameter(Mandatory)][string]$Replacement
  )

  $fullPath = Join-Path $projectRoot $Path
  $content = [IO.File]::ReadAllText($fullPath)
  $regex = [regex]::new($Pattern)
  if (-not $regex.IsMatch($content)) {
    throw "Could not locate version field in $Path"
  }
  $updated = $regex.Replace($content, $Replacement, 1)
  [IO.File]::WriteAllText($fullPath, $updated, $utf8NoBom)
}

Set-VersionMatch "gui\package.json" '(?m)^(\s*"version"\s*:\s*)"\d+\.\d+\.\d+"' ('${1}"' + $Version + '"')
Set-VersionMatch "gui\package-lock.json" '(?m)^(\s*"version"\s*:\s*)"\d+\.\d+\.\d+"' ('${1}"' + $Version + '"')
Set-VersionMatch "gui\package-lock.json" '(?s)("packages"\s*:\s*\{\s*""\s*:\s*\{\s*"name"\s*:\s*"tdl-media-gui"\s*,\s*"version"\s*:\s*)"\d+\.\d+\.\d+"' ('${1}"' + $Version + '"')
Set-VersionMatch "gui\src-tauri\Cargo.toml" '(?m)^(version\s*=\s*)"\d+\.\d+\.\d+"' ('${1}"' + $Version + '"')
Set-VersionMatch "gui\src-tauri\Cargo.lock" '(?s)(name\s*=\s*"tdl-media-gui"\s*\r?\nversion\s*=\s*)"\d+\.\d+\.\d+"' ('${1}"' + $Version + '"')
Set-VersionMatch "gui\src-tauri\tauri.conf.json" '(?m)^(\s*"version"\s*:\s*)"\d+\.\d+\.\d+"' ('${1}"' + $Version + '"')
