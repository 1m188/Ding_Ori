# Switch working directory to the script's own directory
Set-Location $PSScriptRoot

# Publish
dotnet publish OriTrainer.sln `
    -p:PlatformTarget=x86 `
    -c Release `
    --no-self-contained

# Exit code
exit $LASTEXITCODE
