# Windows Installer Build

## Requirements

1. Win 10+
2. Golang
3. MingW
4. Dotnet SDK (if AzureSignTool)
5. AzureSignTool (optional)
6. WiX Toolset

## One off build (-dev output, unsigned)

```
.\build.ps1 <version>
```

For example:
```
.\build.ps1 4.2.0
```
