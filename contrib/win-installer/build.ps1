function ExitOnError() {
    if ($LASTEXITCODE -ne 0) {
        Exit 1
    }
}

function FetchPanel() {
    Remove-Item -Recurse -Force -Path fetch -ErrorAction SilentlyContinue | Out-Null
    New-Item -Force -ItemType Directory fetch | Out-Null
    Push-Location fetch

    $ProgressPreference = 'SilentlyContinue'
    Invoke-WebRequest -UseBasicParsing -OutFile nuget.exe -ErrorAction Stop `
        -Uri https://dist.nuget.org/win-x86-commandline/latest/nuget.exe

    .\nuget.exe install PanelSwWixExtension
    $code = $LASTEXITCODE
    Pop-Location
    if ($code -gt 0) {
        Exit 1
    }
    $loc = Get-ChildItem -Recurse -Path fetch -Name PanelSwWixExtension.dll
    if (!$loc) {
        Write-Host "Could not locate PanelSwWixExtension.dll"
        Exit 1
    }

    Copy-Item -Path fetch/$loc -Destination artifacts/PanelSwWixExtension.dll -ErrorAction Stop
}

function SignItem() {
    param(
        [Parameter(Mandatory)]
        [string[]]$fileNames
    )

    foreach ($val in $ENV:APP_ID, $ENV:TENANT_ID, $ENV:CLIENT_SECRET, $ENV:CERT_NAME) {
        if (!$val) {
            Write-Host "Skipping signing (no config)"
            Return
        }
    }

    CheckCommand AzureSignTool.exe "AzureSignTool"

    AzureSignTool.exe sign -du "https://github.com/containers/podman" `
        -kvu "https://$ENV:VAULT_ID.vault.azure.net" `
        -kvi $ENV:APP_ID `
        -kvt $ENV:TENANT_ID `
        -kvs $ENV:CLIENT_SECRET `
        -kvc $ENV:CERT_NAME `
        -tr http://timestamp.digicert.com $fileNames

    ExitOnError
}

function CheckCommand() {
    param(
        [Parameter(Mandatory)]
        [string] $cmd,
        [Parameter(Mandatory)]
        [string] $description
    )

    if (! (Get-Command $cmd -errorAction SilentlyContinue)) {
        Write-Host "Required dep `"$description`" is not installed"
        Exit 1
    }
}

function CheckRequirements() {
    CheckCommand "dotnet" ".NET SDK"
    CheckCommand "gcc" "MingW CC"
    CheckCommand "candle" "WiX Toolset"
    CheckCommand "go" "Golang"
}


if ($args.Count -lt 1 -or $args[0].Length -lt 1) {
    Write-Host "Usage: " $MyInvocation.MyCommand.Name "<version> [dev | (prod [check])]"
    Exit 1
}

$Env:Path="$Env:Path;C:\Program Files (x86)\WiX Toolset v3.11\bin"

CheckRequirements

$version = $args[0]

if ($version[0] -eq "v") {
    $version = $version.Substring(1)
}

$suffix = "-dev"
if ($args.Count -gt 1 -and $args[1] -eq "prod") {
    $suffix = ""
}

.\process-release.ps1 $version
if ($LASTEXITCODE -eq 2) {
    Write-Host "Skip signaled, relaying skip"
    Exit 2
}
if ($ENV:INSTVER -eq "") {
    Write-Host "process-release did not define an install version!"
    Exit 1
}

FetchPanel

.\build-hooks.bat; ExitOnError
SignItem @("artifacts/win-sshproxy.exe",
          "artifacts/podman.exe",
          "artifacts/podman-msihooks.dll",
          "artifacts/podman-wslkerninst.exe")

.\build-msi.bat $ENV:INSTVER; ExitOnError
SignItem @("podman.msi")

.\build-burn.bat $ENV:INSTVER; ExitOnError
insignia -ib podman-setup.exe -o engine.exe; ExitOnError
SignItem @("engine.exe")

insignia -ab engine.exe podman-setup.exe -o podman-$version$suffix-setup.exe; ExitOnError
SignItem @("podman-$version$suffix-setup.exe")

Write-Host "Complete"
Get-ChildItem "podman-$version$suffix-setup.exe"