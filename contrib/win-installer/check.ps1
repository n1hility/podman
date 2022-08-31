function SkipExists {
    param(
        [Parameter(Mandatory)]
        [string]$url
    )
    try {
        Invoke-WebRequest -Method HEAD -UseBasicParsing -ErrorAction Stop -Uri $url
        Write-Host "Installer already uploaded, skipping"
        Exit 2
    } Catch {
        if ($_.Exception.Response.StatusCode -eq 404) {
            Write-Host "Installer does not exist,  continuing..."
            Return
        }

        throw $_.Exception
    }
}

if ($args.Count -lt 1 -or $args[0].Length -lt 2) {
    Write-Host "Usage: " $MyInvocation.MyCommand.Name "<version>"
    Exit 1
}

$release = $args[0]
$version = $release
if ($release[0] -eq "v") {
    $version = $release.Substring(1)
}

$base_url = "$ENV:FETCH_BASE_URL"
if ($base_url.Length -le 0) {
    $base_url = "https://github.com/containers/podman"
}

$ENV:UPLOAD_ASSET_NAME = "podman-$version-setup.exe"
SkipExists "$base_url/releases/download/$release/podman-$version-setup.exe"