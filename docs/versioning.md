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

The module path and repo identity are `github.com/Terru03/google-photos-takeout-helper`. Release archives include the desktop app and `gtf-cli`. The desktop binary keeps the legacy `GoogleTakeoutFixer` name for now so existing shortcuts, scripts, and bundled dependency layouts do not break during the rebrand.
