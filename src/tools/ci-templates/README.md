# Inactive CI templates

GitHub Actions workflow templates are stored in `workflows/`, outside `.github`, and are intentionally inactive. Pushing this directory does not schedule, trigger or enable them.

`workflows/codeql.yml` retains the Go and JavaScript/TypeScript scan setup. Its configuration is in `codeql/config.yml`; local CodeQL scans can use that configuration without activating GitHub Actions. Existing scan evidence remains in `docs/evidence/bonuses/`.

Keep the templates here until activation is explicitly requested. Activation would require copying the chosen template into `.github/workflows/`; the referenced `src/tools/ci-templates/codeql/config.yml` can stay here.
