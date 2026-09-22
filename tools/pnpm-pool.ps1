# Use the checkout-local pnpm 9 without changing the user's global package manager.
# No param block: forward all arguments verbatim, including short switches such as -v.
$PnpmPoolArguments = @($args)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$PnpmPoolRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$PnpmPoolBin = Join-Path $PnpmPoolRoot '.dev\runtime\pnpm\bin'
$PnpmPoolEntry = Join-Path $PnpmPoolBin 'pnpm.cjs'
$PnpmPoolFrontend = Join-Path $PnpmPoolRoot 'frontend'
$PnpmPoolNode = Get-Command node.exe -ErrorAction SilentlyContinue
if (-not $PnpmPoolNode) { throw 'Node.js is required. See LOCAL_DEVELOPMENT.md.' }
if (-not (Test-Path -LiteralPath $PnpmPoolEntry -PathType Leaf)) {
    throw 'Local pnpm 9 is missing from .dev/runtime/pnpm. See LOCAL_DEVELOPMENT.md.'
}

# npm scripts may invoke pnpm again. Keep those calls on this same local version.
$PnpmPoolShim = Join-Path $PnpmPoolBin 'pnpm.cmd'
$PnpmPoolShimText = "@echo off`r`nnode.exe `"%~dp0pnpm.cjs`" %*`r`nexit /b %errorlevel%`r`n"
if (-not (Test-Path -LiteralPath $PnpmPoolShim) -or
    (Get-Content -LiteralPath $PnpmPoolShim -Raw) -ne $PnpmPoolShimText) {
    Set-Content -LiteralPath $PnpmPoolShim -Value $PnpmPoolShimText -Encoding ASCII -NoNewline
}

$PnpmPoolPreviousPath = $env:Path
$PnpmPoolPreviousHome = [Environment]::GetEnvironmentVariable('PNPM_HOME', 'Process')
$PnpmPoolPreviousStore = [Environment]::GetEnvironmentVariable('npm_config_store_dir', 'Process')
$PnpmPoolExitCode = 1
Push-Location -LiteralPath $PnpmPoolFrontend
try {
    $env:Path = $PnpmPoolBin + [IO.Path]::PathSeparator + $PnpmPoolPreviousPath
    $env:PNPM_HOME = $PnpmPoolBin
    $env:npm_config_store_dir = Join-Path $PnpmPoolRoot '.dev\cache\pnpm'
    & $PnpmPoolNode.Source $PnpmPoolEntry @PnpmPoolArguments
    $PnpmPoolExitCode = $LASTEXITCODE
}
finally {
    Pop-Location
    $env:Path = $PnpmPoolPreviousPath
    if ($null -eq $PnpmPoolPreviousHome) {
        Remove-Item Env:PNPM_HOME -ErrorAction SilentlyContinue
    } else {
        $env:PNPM_HOME = $PnpmPoolPreviousHome
    }
    if ($null -eq $PnpmPoolPreviousStore) {
        Remove-Item Env:npm_config_store_dir -ErrorAction SilentlyContinue
    } else {
        $env:npm_config_store_dir = $PnpmPoolPreviousStore
    }
}
exit $PnpmPoolExitCode
