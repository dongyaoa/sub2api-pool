# Local development lifecycle for this checkout only. Compatible with Windows PowerShell 5.1.
[CmdletBinding()]
param(
    [ValidateSet('Start', 'Stop', 'Status', 'Build', 'RestartBackend')]
    [string]$Action = 'Start'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$RepoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$PhysicalDevRoot = Join-Path $RepoRoot '.dev'
$DevRoot = $PhysicalDevRoot
$RuntimeRootFile = Join-Path $PhysicalDevRoot 'runtime-root.txt'
if (Test-Path -LiteralPath $RuntimeRootFile) {
    # Native PostgreSQL on Windows needs an ASCII path. The junction still stores all
    # files in this checkout's .dev directory; never accept a link to another instance.
    $RuntimeRoot = (Get-Content -LiteralPath $RuntimeRootFile -Raw).Trim()
    if (-not [IO.Path]::IsPathRooted($RuntimeRoot)) { throw 'runtime-root.txt must contain an absolute junction path.' }
    $RuntimeLink = Get-Item -LiteralPath $RuntimeRoot -Force
    if ($RuntimeLink.LinkType -ne 'Junction' -or @($RuntimeLink.Target).Count -ne 1 -or
        [IO.Path]::GetFullPath([string]@($RuntimeLink.Target)[0]).TrimEnd('\') -ne $PhysicalDevRoot.TrimEnd('\')) {
        throw 'Runtime junction does not point to this checkout .dev directory. Refusing to use it.'
    }
    $DevRoot = [IO.Path]::GetFullPath($RuntimeRoot).TrimEnd('\')
}
$RunDir = Join-Path $DevRoot 'run'
$LogDir = Join-Path $DevRoot 'logs'
$AppData = Join-Path $DevRoot 'data\app'
$PgData = Join-Path $DevRoot 'data\postgres'
$RedisData = Join-Path $DevRoot 'data\redis'
$PgBin = Join-Path $DevRoot 'runtime\postgres\bin'
$RedisBin = Join-Path $DevRoot 'runtime\redis'
$BackendExe = Join-Path $DevRoot 'bin\sub2api-pool.exe'
$FrontendDir = Join-Path $RepoRoot 'frontend'
$ViteEntry = Join-Path $FrontendDir 'node_modules\vite\bin\vite.js'
$SecretsFile = Join-Path $DevRoot 'local-secrets.json'
$Ports = [ordered]@{ postgres = 5433; redis = 6380; backend = 8081; frontend = 3001 }
$Executables = @{
    postgres = Join-Path $PgBin 'postgres.exe'
    redis = Join-Path $RedisBin 'redis-server.exe'
    backend = $BackendExe
}
$NodeCommand = Get-Command node.exe -ErrorAction SilentlyContinue
if ($NodeCommand) { $Executables.frontend = $NodeCommand.Source }

function Require-File([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Required file is missing: $Path. See LOCAL_DEVELOPMENT.md."
    }
}

function Get-RecordPath([string]$Name) { Join-Path $RunDir "$Name.json" }

function Get-CanonicalPath([string]$Path) {
    $FullPath = [IO.Path]::GetFullPath($Path)
    if ($DevRoot -ne $PhysicalDevRoot -and $FullPath.StartsWith($DevRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        return $PhysicalDevRoot + $FullPath.Substring($DevRoot.Length)
    }
    return $FullPath
}

function Read-Record([string]$Name) {
    $RecordPath = Get-RecordPath $Name
    if (-not (Test-Path -LiteralPath $RecordPath)) { return $null }
    $Record = Get-Content -LiteralPath $RecordPath -Raw | ConvertFrom-Json
    if ($Record.RepoRoot -ne $RepoRoot -or $Record.Name -ne $Name) {
        throw "Refusing an invalid process record: $RecordPath"
    }
    if ($Name -ne 'frontend' -and (Get-CanonicalPath $Record.Executable) -ne (Get-CanonicalPath $Executables[$Name])) {
        throw "Executable in process record does not belong to this checkout: $RecordPath"
    }
    if ($Name -eq 'frontend' -and $Record.EntryPoint -ne $ViteEntry) {
        throw "Frontend process record belongs to another checkout: $RecordPath"
    }
    return $Record
}

function Get-VerifiedProcess($Record) {
    if ($null -eq $Record) { return $null }
    $Process = Get-Process -Id ([int]$Record.ProcessId) -ErrorAction SilentlyContinue
    if (-not $Process) { return $null }
    try {
        $Process.Refresh()
        $ObservedPath = $Process.Path
        if ([string]::IsNullOrWhiteSpace($ObservedPath)) { return $null }
        if ((Get-CanonicalPath $ObservedPath) -ne (Get-CanonicalPath $Record.Executable) -or
            $Process.StartTime.ToUniversalTime().Ticks -ne [long]$Record.StartTimeUtcTicks) {
            return $null
        }
    } catch { return $null }
    return $Process
}

function Get-PortListeners([int]$Port) {
    # Query all listeners so a failure to enumerate sockets is not mistaken for a free port.
    @(Get-NetTCPConnection -State Listen -ErrorAction Stop | Where-Object LocalPort -eq $Port)
}

function Assert-PortOwnership([string]$Name, $Process) {
    foreach ($Listener in @(Get-PortListeners $Ports[$Name])) {
        if (-not $Process -or $Listener.OwningProcess -ne $Process.Id) {
            throw "Port $($Ports[$Name]) is occupied by PID $($Listener.OwningProcess), which is not the recorded pool $Name process. Refusing to use or stop that listener."
        }
    }
}

function Save-Record([string]$Name, $Process) {
    $Record = [ordered]@{
        RepoRoot = $RepoRoot
        Name = $Name
        ProcessId = $Process.Id
        Executable = Get-CanonicalPath $Executables[$Name]
        StartTimeUtcTicks = $Process.StartTime.ToUniversalTime().Ticks.ToString()
        EntryPoint = $(if ($Name -eq 'frontend') { $ViteEntry } else { '' })
    }
    $Record | ConvertTo-Json | Set-Content -LiteralPath (Get-RecordPath $Name) -Encoding UTF8
}

function Invoke-WithEnvironment([hashtable]$Values, [scriptblock]$Body) {
    $Saved = @{}
    foreach ($Key in $Values.Keys) {
        $Saved[$Key] = [Environment]::GetEnvironmentVariable($Key, 'Process')
        [Environment]::SetEnvironmentVariable($Key, $Values[$Key], 'Process')
    }
    try { & $Body } finally {
        foreach ($Key in $Saved.Keys) {
            if ($null -eq $Saved[$Key]) {
                Remove-Item -LiteralPath "Env:\$Key" -ErrorAction SilentlyContinue
            } else {
                [Environment]::SetEnvironmentVariable($Key, $Saved[$Key], 'Process')
            }
        }
    }
}

function Quote-Argument([string]$Value) {
    # Windows CommandLineToArgvW quoting, including trailing backslashes.
    '"' + [regex]::Replace([regex]::Replace($Value, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
}

function Start-PoolProcess([string]$Name, [string[]]$Arguments, [string]$WorkingDirectory, [hashtable]$Environment) {
    $Existing = Get-VerifiedProcess (Read-Record $Name)
    Assert-PortOwnership $Name $Existing
    if ($Existing) {
        Write-Host "$Name already running (PID $($Existing.Id))."
        return $false
    }
    $Parameters = @{
        FilePath = $Executables[$Name]
        WorkingDirectory = $WorkingDirectory
        WindowStyle = 'Hidden'
        PassThru = $true
        RedirectStandardOutput = Join-Path $LogDir "$Name.stdout.log"
        RedirectStandardError = Join-Path $LogDir "$Name.stderr.log"
    }
    if (@($Arguments).Count -gt 0) {
        $Parameters.ArgumentList = ($Arguments | ForEach-Object { Quote-Argument $_ }) -join ' '
    }
    $Started = Invoke-WithEnvironment $Environment { Start-Process @Parameters }
    # Retain identity immediately; never infer ownership later from a port or process name.
    try {
        Save-Record $Name $Started
        Require-File (Get-RecordPath $Name)
    } catch {
        $RecordingError = $_.Exception.Message
        # Use the process object returned by Start-Process, never rediscover a PID here.
        try {
            if (-not $Started.HasExited) {
                Stop-Process -InputObject $Started -Force -ErrorAction Stop
                $null = $Started.WaitForExit(10000)
            }
        } catch { Write-Warning "Could not stop the newly launched $Name process after its record failed: $($_.Exception.Message)" }
        throw "Could not record the newly launched $Name process: $RecordingError"
    }
    Write-Host "Started $Name (PID $($Started.Id))."
    return $true
}

function Wait-PoolReady([string]$Name, [string]$Url = '', [int]$TimeoutSeconds = 180) {
    $Deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $Process = Get-VerifiedProcess (Read-Record $Name)
        if (-not $Process) { throw "$Name exited. Inspect $LogDir\$Name.stderr.log and $Name.stdout.log." }
        Assert-PortOwnership $Name $Process
        $Listening = @(Get-PortListeners $Ports[$Name]).Count -gt 0
        if ($Listening) {
            if ($Name -eq 'postgres') {
                # This runtime bundles server utilities only. A verified listener plus
                # pg_ctl status checks liveness; AUTO_SETUP validates the SQL connection.
                & (Join-Path $PgBin 'pg_ctl.exe') status -D $PgData *> $null
                if ($LASTEXITCODE -eq 0) { return }
            } elseif ($Name -eq 'redis') {
                $Secrets = Read-Secrets
                $RedisReady = Invoke-WithEnvironment @{ REDISCLI_AUTH = [string]$Secrets.RedisPassword } {
                    $Reply = & (Join-Path $RedisBin 'redis-cli.exe') -h 127.0.0.1 -p 6380 ping
                    $LASTEXITCODE -eq 0 -and $Reply -eq 'PONG'
                }
                if ($RedisReady) { return }
            } else {
                try {
                    $Response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 5
                    if ($Response.StatusCode -eq 200) { return }
                } catch { }
            }
        }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $Deadline)
    throw "$Name was not ready within $TimeoutSeconds seconds. Inspect $LogDir."
}

function Stop-VerifiedTree($Record) {
    $RootProcess = Get-VerifiedProcess $Record
    if (-not $RootProcess) { return }
    # Record descendants while their ancestry is still verifiable, then check each identity again.
    $Candidates = @(Get-CimInstance Win32_Process)
    $Known = @($Record)
    for ($Index = 0; $Index -lt $Known.Count; $Index++) {
        $Parent = $Known[$Index]
        if (-not (Get-VerifiedProcess $Parent)) { continue }
        foreach ($Candidate in @($Candidates | Where-Object ParentProcessId -eq $Parent.ProcessId)) {
            $Child = Get-Process -Id $Candidate.ProcessId -ErrorAction SilentlyContinue
            if (-not $Child) { continue }
            try {
                $Ticks = $Child.StartTime.ToUniversalTime().Ticks
                if ($Ticks -lt [long]$Parent.StartTimeUtcTicks -or -not $Child.Path) { continue }
                # A PID may be reused between the CIM snapshot and Get-Process.
                # CIM records microseconds, while StartTime keeps 100-nanosecond ticks.
                if (-not $Candidate.CreationDate) { continue }
                $SnapshotTicks = ([datetime]$Candidate.CreationDate).ToUniversalTime().Ticks
                $ProcessMicrosecondTicks = $Ticks - ($Ticks % 10)
                if ($SnapshotTicks -ne $ProcessMicrosecondTicks) { continue }
                if (-not (Get-VerifiedProcess $Parent)) { break }
                if ($Known.ProcessId -contains $Child.Id) { continue }
                $Known += [pscustomobject]@{
                    ProcessId = $Child.Id
                    Executable = Get-CanonicalPath $Child.Path
                    StartTimeUtcTicks = $Ticks.ToString()
                }
            } catch { continue }
        }
    }
    $Known | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $RunDir "$($Record.Name)-stopping.json") -Encoding UTF8
    for ($Index = $Known.Count - 1; $Index -ge 0; $Index--) {
        $Process = Get-VerifiedProcess $Known[$Index]
        if ($Process) {
            Stop-Process -InputObject $Process -Force -ErrorAction Stop
            $null = $Process.WaitForExit(10000)
        }
    }
    Remove-Item -LiteralPath (Join-Path $RunDir "$($Record.Name)-stopping.json") -Force
}

function Stop-PoolProcess([string]$Name) {
    $Record = Read-Record $Name
    $Process = Get-VerifiedProcess $Record
    if (-not $Process) {
        if ($Record) { Remove-Item -LiteralPath (Get-RecordPath $Name) -Force }
        Write-Host "$Name is stopped (no verified process)."
        return
    }
    Assert-PortOwnership $Name $Process
    if ($Name -eq 'postgres') {
        Require-File (Join-Path $PgBin 'pg_ctl.exe')
        $PostmasterFile = Join-Path $PgData 'postmaster.pid'
        Require-File $PostmasterFile
        $PostmasterId = [int](Get-Content -LiteralPath $PostmasterFile -TotalCount 1)
        if ($PostmasterId -ne $Process.Id) { throw 'PostgreSQL data directory PID does not match its process record. Refusing to stop it.' }
        & (Join-Path $PgBin 'pg_ctl.exe') stop -D $PgData -m fast -w -t 30
        if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL did not shut down cleanly. Its process record has been retained.' }
    } elseif ($Name -eq 'redis') {
        Require-File (Join-Path $RedisBin 'redis-cli.exe')
        $Secrets = Read-Secrets
        Invoke-WithEnvironment @{ REDISCLI_AUTH = [string]$Secrets.RedisPassword } {
            & (Join-Path $RedisBin 'redis-cli.exe') -h 127.0.0.1 -p 6380 shutdown save
            if ($LASTEXITCODE -ne 0) { throw 'Redis did not shut down cleanly. Its process record has been retained.' }
        }
        if (-not $Process.WaitForExit(30000)) { throw 'Redis shutdown timed out. Its process record has been retained.' }
    } else {
        Stop-VerifiedTree $Record
    }
    if (Get-VerifiedProcess $Record) { throw "$Name is still running; its process record has been retained." }
    Remove-Item -LiteralPath (Get-RecordPath $Name) -Force
    Write-Host "Stopped $Name."
}

function Read-Secrets {
    Require-File $SecretsFile
    $Secrets = Get-Content -LiteralPath $SecretsFile -Raw | ConvertFrom-Json
    foreach ($Field in @('DatabasePassword', 'RedisPassword', 'AdminEmail', 'AdminPassword', 'JwtSecret', 'TotpEncryptionKey')) {
        if (-not $Secrets.PSObject.Properties[$Field] -or [string]::IsNullOrWhiteSpace([string]$Secrets.$Field)) {
            throw "Missing $Field in $SecretsFile. Secret values are never printed by this script."
        }
    }
    return $Secrets
}

function Get-BackendEnvironment($Secrets) {
    @{
        DATA_DIR = $AppData
        CONFIG_FILE = Join-Path $AppData 'config.yaml'
        AUTO_SETUP = 'true'; SKIP_SETUP = 'false'; RUN_MODE = 'standard'
        SERVER_HOST = '127.0.0.1'; SERVER_PORT = '8081'; SERVER_MODE = 'release'
        SERVER_FRONTEND_URL = 'http://localhost:3001'
        DATABASE_HOST = '127.0.0.1'; DATABASE_PORT = '5433'
        DATABASE_USER = 'sub2api_pool'; DATABASE_DBNAME = 'sub2api_pool'
        DATABASE_PASSWORD = [string]$Secrets.DatabasePassword; DATABASE_SSLMODE = 'disable'
        REDIS_HOST = '127.0.0.1'; REDIS_PORT = '6380'; REDIS_USERNAME = ''
        REDIS_PASSWORD = [string]$Secrets.RedisPassword; REDIS_DB = '0'; REDIS_ENABLE_TLS = 'false'
        ADMIN_EMAIL = [string]$Secrets.AdminEmail; ADMIN_PASSWORD = [string]$Secrets.AdminPassword
        JWT_SECRET = [string]$Secrets.JwtSecret; JWT_EXPIRE_HOUR = '24'
        TOTP_ENCRYPTION_KEY = [string]$Secrets.TotpEncryptionKey
        ZONEINFO = Join-Path $DevRoot 'runtime\go\lib\time\zoneinfo.zip'
        PRICING_FALLBACK_FILE = Join-Path $RepoRoot 'backend\resources\model-pricing\model_prices_and_context_window.json'
        PRICING_DATA_DIR = Join-Path $AppData 'data'
        TZ = 'Asia/Shanghai'
    }
}

function Restart-PoolBackend {
    # Only replace the recorded backend; PostgreSQL, Redis and Vite stay running.
    foreach ($Name in @('postgres', 'redis', 'backend')) {
        Assert-PortOwnership $Name (Get-VerifiedProcess (Read-Record $Name))
    }
    $BackendEnvironment = Get-BackendEnvironment (Read-Secrets)
    $StagedBackend = Build-PoolBackend -ForRestart
    $BackupBackend = Join-Path $DevRoot 'bin\sub2api-pool-before-restart.exe'
    try {
        Copy-Item -LiteralPath $BackendExe -Destination $BackupBackend -Force
        Stop-PoolProcess 'backend'
        try {
            Copy-Item -LiteralPath $StagedBackend -Destination $BackendExe -Force
            $null = Start-PoolProcess 'backend' @() $AppData $BackendEnvironment
            Wait-PoolReady 'backend' 'http://127.0.0.1:8081/health'
        } catch {
            $RestartError = $_
            Stop-PoolProcess 'backend'
            Copy-Item -LiteralPath $BackupBackend -Destination $BackendExe -Force
            $null = Start-PoolProcess 'backend' @() $AppData $BackendEnvironment
            Wait-PoolReady 'backend' 'http://127.0.0.1:8081/health'
            throw $RestartError
        }
    } finally {
        if (Test-Path -LiteralPath $StagedBackend) { Remove-Item -LiteralPath $StagedBackend -Force }
    }
    Show-PoolStatus
}

function Show-PoolStatus {
    foreach ($Name in $Ports.Keys) {
        $Process = Get-VerifiedProcess (Read-Record $Name)
        $Listeners = @(Get-PortListeners $Ports[$Name])
        $Status = 'stopped'
        if ($Process) { $Status = "running (PID $($Process.Id))" }
        if ($Listeners.Count -gt 0) {
            $Foreign = @($Listeners | Where-Object { -not $Process -or $_.OwningProcess -ne $Process.Id })
            if ($Foreign.Count -gt 0) { $Status = "CONFLICT: unowned listener PID(s) $(($Foreign.OwningProcess | Select-Object -Unique) -join ', ')" }
        } elseif ($Process) { $Status += ', not listening yet' }
        Write-Host ('{0,-10} {1,-6} {2}' -f $Name, $Ports[$Name], $Status)
    }
    Write-Host "Website: http://localhost:3001"
    Write-Host "Data: $PhysicalDevRoot\data"
    Write-Host "Logs: $PhysicalDevRoot\logs"
}

function Build-PoolBackend {
    param([switch]$ForRestart)
    $Running = Get-VerifiedProcess (Read-Record 'backend')
    if ($Running -and -not $ForRestart) { throw 'Use -Action RestartBackend to build and replace the running pool backend.' }
    Assert-PortOwnership 'backend' $Running
    $GoRoot = Join-Path $DevRoot 'runtime\go'
    $GoExe = Join-Path $GoRoot 'bin\go.exe'
    Require-File $GoExe
    $GoEnvironment = @{
        GOROOT = $GoRoot; GOCACHE = Join-Path $DevRoot 'cache\go-build'
        GOMODCACHE = Join-Path $DevRoot 'cache\go-mod'; GOTMPDIR = Join-Path $DevRoot 'tmp'
        GOPATH = Join-Path $DevRoot 'cache\gopath'; GOENV = 'off'; GOWORK = 'off'
        GOTOOLCHAIN = 'local'; CGO_ENABLED = '0'; GOFLAGS = ''; GOOS = ''; GOARCH = ''
        GOMAXPROCS = '2'
    }
    foreach ($Directory in @((Split-Path $BackendExe -Parent), $GoEnvironment.GOCACHE, $GoEnvironment.GOMODCACHE, $GoEnvironment.GOTMPDIR, $GoEnvironment.GOPATH)) {
        $null = New-Item -ItemType Directory -Path $Directory -Force
    }
    $BuildOutput = Join-Path (Split-Path $BackendExe -Parent) ("sub2api-pool-build-{0}.exe" -f [guid]::NewGuid().ToString('N'))
    Push-Location (Join-Path $RepoRoot 'backend')
    try {
        Invoke-WithEnvironment $GoEnvironment {
            & $GoExe build -p 1 -trimpath -o $BuildOutput ./cmd/server
            if ($LASTEXITCODE -ne 0) { throw "Go build failed (exit $LASTEXITCODE). Existing backend executable was preserved." }
        }
        if ($ForRestart) {
            Write-Host "Backend staged: $BuildOutput"
            return $BuildOutput
        } else {
            Move-Item -LiteralPath $BuildOutput -Destination $BackendExe -Force
            Write-Host "Backend built: $BackendExe"
        }
    } finally {
        Pop-Location
        if (-not $ForRestart -and (Test-Path -LiteralPath $BuildOutput)) { Remove-Item -LiteralPath $BuildOutput -Force }
    }
}

function Start-Pool {
    $Secrets = Read-Secrets
    if (-not $Executables.ContainsKey('frontend')) { throw 'node.exe is not available on PATH. Install Node.js before starting.' }
    foreach ($Executable in $Executables.Values) { Require-File $Executable }
    Require-File (Join-Path $PgBin 'pg_ctl.exe')
    Require-File (Join-Path $RedisBin 'redis-cli.exe')
    Require-File (Join-Path $DevRoot 'runtime\go\lib\time\zoneinfo.zip')
    Require-File (Join-Path $RepoRoot 'backend\resources\model-pricing\model_prices_and_context_window.json')
    Require-File $ViteEntry
    Require-File (Join-Path $PgData 'PG_VERSION')
    Require-File (Join-Path $RedisData 'redis.conf')
    foreach ($Name in $Ports.Keys) { Assert-PortOwnership $Name (Get-VerifiedProcess (Read-Record $Name)) }
    foreach ($Directory in @($RunDir, $LogDir, $AppData)) {
        $null = New-Item -ItemType Directory -Path $Directory -Force
    }
    $StartedNames = [Collections.Generic.List[string]]::new()
    try {
        if (Start-PoolProcess 'postgres' @('-D', $PgData, '-h', '127.0.0.1', '-p', '5433') $PgData @{}) { $StartedNames.Add('postgres') }
        Wait-PoolReady 'postgres'
        # The bundled MSYS Redis interprets Windows absolute config paths as relative.
        if (Start-PoolProcess 'redis' @('redis.conf') $RedisData @{}) { $StartedNames.Add('redis') }
        Wait-PoolReady 'redis'
        if (Start-PoolProcess 'backend' @() $AppData (Get-BackendEnvironment $Secrets)) { $StartedNames.Add('backend') }
        Wait-PoolReady 'backend' 'http://127.0.0.1:8081/health'
        $FrontendEnvironment = @{ VITE_DEV_PORT = '3001'; VITE_DEV_PROXY_TARGET = 'http://127.0.0.1:8081'; VITE_API_BASE_URL = '/api/v1' }
        if (Start-PoolProcess 'frontend' @($ViteEntry, '--host', '127.0.0.1', '--port', '3001', '--strictPort') $FrontendDir $FrontendEnvironment) { $StartedNames.Add('frontend') }
        Wait-PoolReady 'frontend' 'http://127.0.0.1:3001/'
        Show-PoolStatus
    } catch {
        $StartupError = $_
        for ($Index = $StartedNames.Count - 1; $Index -ge 0; $Index--) {
            try { Stop-PoolProcess $StartedNames[$Index] } catch { Write-Warning $_.Exception.Message }
        }
        throw $StartupError
    }
}

# Serialise lifecycle mutations, including two terminals starting at the same time.
$Lock = $null
try {
    if ($Action -ne 'Status') {
        $null = New-Item -ItemType Directory -Path $RunDir -Force
        try {
            $Lock = [IO.File]::Open((Join-Path $RunDir 'lifecycle.lock'), [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        } catch { throw 'Another pool lifecycle command is running. Wait for it to finish and retry.' }
    }
    switch ($Action) {
        'Start' { Start-Pool }
        'Stop' {
            $StopErrors = @()
            foreach ($Name in @('frontend', 'backend', 'redis', 'postgres')) {
                try { Stop-PoolProcess $Name } catch { $StopErrors += $_.Exception.Message }
            }
            if ($StopErrors.Count -gt 0) { throw ($StopErrors -join [Environment]::NewLine) }
        }
        'Status' { Show-PoolStatus }
        'Build' { Build-PoolBackend }
        'RestartBackend' { Restart-PoolBackend }
    }
} catch {
    Write-Error $_.Exception.Message
    exit 1
} finally {
    if ($Lock) { $Lock.Dispose() }
}
