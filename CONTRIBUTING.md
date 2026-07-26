# Contributing

Use Go 1.26.5, add behavior through a red-green-refactor cycle, and preserve the
documented backend continuity contract. Run:

```text
make check
make lint staticcheck nilaway mutation vuln workflows
```

Backend changes require shared conformance plus native fault coverage. Public
API changes require an updated API baseline, documentation, and changelog.
Never weaken owner/token comparisons, safety deadlines, ambiguity handling, or
resource bounds to make a test pass.
