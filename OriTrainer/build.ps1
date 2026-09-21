# Switch working directory to the script's own directory
Set-Location $PSScriptRoot

# Build
dotnet build OriTrainer.sln `
    -p:PlatformTarget=x86

# Exit code
exit $LASTEXITCODE
