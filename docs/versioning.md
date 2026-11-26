# Versioning

## Strategy

`internal/version/version.go` contains a development fallback:

- `dev` for local builds

Release builds overwrite that value through Go linker flags in GitHub Actions:

```text
-X github.com/Terru03/google-photos-takeout-helper/internal/version.Tag=${VERSION}
```

## Practical Rules

- local `go build` => `dev`
- tagged release workflow => tag name such as `v1.4.0`

## Why it works this way

This keeps development builds simple while avoiding manual source edits during release packaging.

## Binary names

The module path and repo identity are `github.com/Terru03/google-photos-takeout-helper`. The `GoogleTakeoutFixer` release binary opens the desktop interface when launched without arguments and runs as a CLI when given arguments.

Build the Windows x64 release archive locally with `./scripts/package-windows-release.ps1 -Version v1.0.0`. The script verifies the upstream ExifTool ZIP against its pinned SHA-256, packages the full runtime, and repairs a synthetic photo from the extracted package before saving the archive in `dist`.
