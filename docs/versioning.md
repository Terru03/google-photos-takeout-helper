# Versioning

## Strategy

`internal/version/version.go` contains a development fallback:

- `dev` for local builds

Release builds overwrite that value through Go linker flags in GitHub Actions:

```text
-X github.com/feloex/GoogleTakeoutFixer/internal/version.Tag=${VERSION}
```

## Practical Rules

- local `go build` => `dev`
- tagged release workflow => tag name such as `v1.4.0`

## Why it works this way

This keeps development builds simple while avoiding manual source edits during release packaging.
