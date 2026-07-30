#requires -version 5.1

[CmdletBinding()]
param(
    [string]$InstallRoot = $(if ($env:LLM_GATEWAY_HOME) {
        $env:LLM_GATEWAY_HOME
    } else {
        Join-Path $env:USERPROFILE '.llm-gateway'
    }),

    [switch]$Force,
    [switch]$NonInteractive,
    [switch]$SkipDownload,
    [switch]$SkipPath
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Set-StrictMode -Version Latest

$script:Repository = 'hwj123hwj/litellm-gateway'
$script:BinaryName = 'gateway.exe'

function Write-InstallerMessage {
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet('Info', 'Success', 'Warning')]
        [string]$Level,

        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    $prefix = switch ($Level) {
        'Info' { '[i]' }
        'Success' { '[OK]' }
        'Warning' { '[!]' }
    }
    Write-Host "  $prefix $Message"
}

function Merge-PathValue {
    param(
        [AllowEmptyString()]
        [string]$PathValue,

        [Parameter(Mandatory = $true)]
        [string]$RequiredEntry
    )

    $entries = New-Object 'System.Collections.Generic.List[string]'
    $seen = New-Object 'System.Collections.Generic.HashSet[string]' (
        [StringComparer]::OrdinalIgnoreCase
    )

    foreach ($rawEntry in ([string]$PathValue -split ';')) {
        $entry = $rawEntry.Trim()
        if ([string]::IsNullOrWhiteSpace($entry)) {
            continue
        }

        $comparisonKey = [Environment]::ExpandEnvironmentVariables(
            $entry
        ).TrimEnd('\', '/')
        if ($seen.Add($comparisonKey)) {
            $entries.Add($entry)
        }
    }

    $normalizedRequiredEntry = $RequiredEntry.Trim().TrimEnd('\', '/')
    $requiredComparisonKey = [Environment]::ExpandEnvironmentVariables(
        $normalizedRequiredEntry
    ).TrimEnd('\', '/')
    if ($seen.Add($requiredComparisonKey)) {
        $entries.Add($normalizedRequiredEntry)
    }

    return $entries -join ';'
}

function Set-GatewayUserPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$InstallDirectory
    )

    $environmentKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey(
        'Environment',
        $true
    )
    if ($null -eq $environmentKey) {
        throw 'Unable to open HKCU\Environment for writing.'
    }

    try {
        $currentUserPath = [string]$environmentKey.GetValue(
            'Path',
            '',
            [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
        )
        $newUserPath = Merge-PathValue `
            -PathValue $currentUserPath `
            -RequiredEntry $InstallDirectory

        if ($newUserPath -cne $currentUserPath) {
            $environmentKey.SetValue(
                'Path',
                $newUserPath,
                [Microsoft.Win32.RegistryValueKind]::ExpandString
            )
            Write-InstallerMessage Success 'User PATH updated and deduplicated.'
        } else {
            Write-InstallerMessage Success 'User PATH is already configured.'
        }
    }
    finally {
        $environmentKey.Dispose()
    }

    $env:Path = Merge-PathValue `
        -PathValue $env:Path `
        -RequiredEntry $InstallDirectory
}

function New-RandomMasterKey {
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    $characters = New-Object Text.StringBuilder

    try {
        while ($characters.Length -lt 32) {
            $bytes = New-Object byte[] 32
            $random.GetBytes($bytes)
            $chunk = [Convert]::ToBase64String($bytes) -replace '[^A-Za-z0-9]', ''
            [void]$characters.Append($chunk)
        }
    }
    finally {
        $random.Dispose()
    }

    return 'sk-' + $characters.ToString().Substring(0, 32)
}

function Write-Utf8FileWithoutBom {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$Content
    )

    [IO.File]::WriteAllText(
        $Path,
        $Content,
        (New-Object Text.UTF8Encoding($false))
    )
}

function New-GatewayConfig {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ConfigPath,

        [switch]$NonInteractiveMode
    )

    if (Test-Path -LiteralPath $ConfigPath) {
        Write-InstallerMessage Success "Config already exists: $ConfigPath"
        return
    }

    $glmKey = ''
    $mimoKey = ''
    $longCatKey = ''
    $easyClawKey = ''
    $choices = @('5')

    if (-not $NonInteractiveMode) {
        Write-Host ''
        Write-Host '  Configure LLM providers (space-separated choices):'
        Write-Host '  1) GLM'
        Write-Host '  2) MiMo'
        Write-Host '  3) LongCat'
        Write-Host '  4) EasyClaw'
        Write-Host '  5) Skip and configure later'
        Write-Host ''

        $choiceInput = Read-Host '  Select [1 2 3 4 / 5]'
        if (-not [string]::IsNullOrWhiteSpace($choiceInput)) {
            $choices = $choiceInput.Trim() -split '\s+'
        }

        if ($choices -notcontains '5') {
            if ($choices -contains '1') {
                $glmKey = Read-Host '  GLM API Key'
            }
            if ($choices -contains '2') {
                $mimoKey = Read-Host '  MiMo API Key'
            }
            if ($choices -contains '3') {
                $longCatKey = Read-Host '  LongCat API Key'
            }
            if ($choices -contains '4') {
                $easyClawKey = Read-Host '  EasyClaw API Key'
            }
        }
    }

    $masterKey = New-RandomMasterKey
    $configLines = @(
        '# LLM Gateway configuration'
        '# Gateway authentication token'
        "LITELLM_MASTER_KEY=$masterKey"
        ''
        '# Provider API keys'
        "GLM_API_KEY=$glmKey"
        "MIMO_API_KEY=$mimoKey"
        "LONGCAT_API_KEY=$longCatKey"
        "EASYCLAW_API_KEY=$easyClawKey"
        ''
        '# Gateway port'
        'PORT=4001'
        ''
        '# Log level'
        'LOG_LEVEL=info'
        ''
    )

    Write-Utf8FileWithoutBom `
        -Path $ConfigPath `
        -Content ($configLines -join "`n")
    Write-InstallerMessage Success "Config created: $ConfigPath"
}

function Invoke-CurlFileDownload {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Uri,

        [Parameter(Mandatory = $true)]
        [string]$Destination
    )

    $curl = Get-Command 'curl.exe' -ErrorAction SilentlyContinue
    if ($null -eq $curl) {
        throw 'curl.exe is not available.'
    }

    & $curl.Source `
        --fail `
        --location `
        --silent `
        --show-error `
        --retry 3 `
        --retry-delay 1 `
        --output $Destination `
        --url $Uri

    if ($LASTEXITCODE -ne 0) {
        throw "curl.exe failed with exit code $LASTEXITCODE while downloading $Uri"
    }
    if (-not (Test-Path -LiteralPath $Destination)) {
        throw "curl.exe completed without creating: $Destination"
    }
    if ((Get-Item -LiteralPath $Destination).Length -eq 0) {
        throw "curl.exe downloaded an empty file from: $Uri"
    }
}

function Invoke-FileDownload {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Uri,

        [Parameter(Mandatory = $true)]
        [string]$Destination
    )

    $temporaryPath = "$Destination.download"
    try {
        if (Get-Command 'curl.exe' -ErrorAction SilentlyContinue) {
            Invoke-CurlFileDownload -Uri $Uri -Destination $temporaryPath
        } else {
            Invoke-WebRequest `
                -UseBasicParsing `
                -Uri $Uri `
                -OutFile $temporaryPath
        }
        Move-Item -LiteralPath $temporaryPath -Destination $Destination -Force
    }
    finally {
        if (Test-Path -LiteralPath $temporaryPath) {
            Remove-Item -LiteralPath $temporaryPath -Force
        }
    }
}

function Write-InstallerFailure {
    param(
        [Parameter(Mandatory = $true)]
        [Management.Automation.ErrorRecord]$ErrorRecord
    )

    $message = $ErrorRecord.Exception.Message
    try {
        [Console]::Error.WriteLine('')
        [Console]::Error.WriteLine("  [ERROR] $message")
        [Console]::Error.WriteLine('')
    }
    catch {
        # Never let console rendering hide the original installer failure.
    }
}

function Install-GatewayBinary {
    param(
        [Parameter(Mandatory = $true)]
        [string]$BinaryPath,

        [switch]$ForceDownload,
        [switch]$NonInteractiveMode,
        [switch]$SkipBinaryDownload
    )

    if ($SkipBinaryDownload) {
        if (-not (Test-Path -LiteralPath $BinaryPath)) {
            throw "SkipDownload was specified but the binary is missing: $BinaryPath"
        }
        Write-InstallerMessage Success "Using existing binary: $BinaryPath"
        return
    }

    $shouldDownload = $ForceDownload -or -not (Test-Path -LiteralPath $BinaryPath)
    if (-not $shouldDownload -and -not $NonInteractiveMode) {
        $answer = Read-Host '  Gateway is already installed. Reinstall/upgrade? [y/N]'
        $shouldDownload = $answer -match '^(?i:y|yes)$'
    }

    if (-not $shouldDownload) {
        Write-InstallerMessage Success "Keeping existing binary: $BinaryPath"
        return
    }

    $architecture = if ($env:PROCESSOR_ARCHITEW6432) {
        $env:PROCESSOR_ARCHITEW6432
    } else {
        $env:PROCESSOR_ARCHITECTURE
    }
    if ($architecture -notmatch '^(?i:AMD64)$') {
        throw "Unsupported Windows architecture: $architecture"
    }

    $downloadUrl = (
        "https://github.com/$script:Repository/releases/latest/download/" +
        'gateway-windows-amd64.exe'
    )
    Write-InstallerMessage Info 'Downloading the latest gateway release...'
    Invoke-FileDownload -Uri $downloadUrl -Destination $BinaryPath
    Write-InstallerMessage Success 'Gateway binary downloaded.'
}

function Write-GatewayLauncher {
    param(
        [Parameter(Mandatory = $true)]
        [string]$LauncherPath,

        [Parameter(Mandatory = $true)]
        [string]$GatewayHome,

        [Parameter(Mandatory = $true)]
        [string]$BinaryPath
    )

    $launcher = @(
        '@echo off'
        ('cd /d "{0}"' -f $GatewayHome)
        ('"{0}" %*' -f $BinaryPath)
        ''
    ) -join "`r`n"

    Write-Utf8FileWithoutBom -Path $LauncherPath -Content $launcher
    Write-InstallerMessage Success "Launcher created: $LauncherPath"
}

function Invoke-GatewayInstaller {
    if ($env:OS -ne 'Windows_NT') {
        throw 'This installer supports Windows only. Use scripts/install.sh on Linux or macOS.'
    }

    [Net.ServicePointManager]::SecurityProtocol = (
        [Net.ServicePointManager]::SecurityProtocol -bor
        [Net.SecurityProtocolType]::Tls12
    )

    $resolvedInstallRoot = [IO.Path]::GetFullPath(
        [Environment]::ExpandEnvironmentVariables($InstallRoot)
    )
    $installDirectory = Join-Path $resolvedInstallRoot 'bin'
    $binaryPath = Join-Path $installDirectory $script:BinaryName
    $configPath = Join-Path $resolvedInstallRoot '.env'
    $providersPath = Join-Path $resolvedInstallRoot 'providers.yaml'
    $launcherPath = Join-Path $installDirectory 'llm-gateway.cmd'

    Write-Host ''
    Write-Host '  ========================================'
    Write-Host '       LLM Gateway Windows Installer'
    Write-Host '  ========================================'
    Write-Host ''
    Write-InstallerMessage Info 'Platform: windows/amd64'

    New-Item -ItemType Directory -Path $installDirectory -Force | Out-Null
    Write-InstallerMessage Success "Install directory: $installDirectory"

    Install-GatewayBinary `
        -BinaryPath $binaryPath `
        -ForceDownload:$Force `
        -NonInteractiveMode:$NonInteractive `
        -SkipBinaryDownload:$SkipDownload

    if (-not $SkipPath) {
        Set-GatewayUserPath -InstallDirectory $installDirectory
    } else {
        Write-InstallerMessage Info 'Skipping PATH update.'
    }

    if (-not (Test-Path -LiteralPath $providersPath)) {
        Write-InstallerMessage Info 'Downloading providers.yaml...'
        Invoke-FileDownload `
            -Uri "https://raw.githubusercontent.com/$script:Repository/main/go-gateway/providers.yaml" `
            -Destination $providersPath
        Write-InstallerMessage Success 'providers.yaml downloaded.'
    } else {
        Write-InstallerMessage Success 'providers.yaml already exists.'
    }

    New-GatewayConfig `
        -ConfigPath $configPath `
        -NonInteractiveMode:$NonInteractive
    Write-GatewayLauncher `
        -LauncherPath $launcherPath `
        -GatewayHome $resolvedInstallRoot `
        -BinaryPath $binaryPath

    Write-Host ''
    Write-InstallerMessage Success 'Installation completed.'
    Write-Host '  Open a new terminal, then run: llm-gateway'
    Write-Host "  Config: $configPath"
    Write-Host ''
}

if ($env:LLM_GATEWAY_INSTALLER_TEST_MODE -ne '1') {
    try {
        Invoke-GatewayInstaller
    }
    catch {
        Write-InstallerFailure -ErrorRecord $_
        exit 1
    }
}
