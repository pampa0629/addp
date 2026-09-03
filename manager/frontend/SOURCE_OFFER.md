# Corresponding source

The preferred form for modifying the ADDP Manager frontend is this repository,
including `manager/frontend`, the repository-local `common-frontend` sources,
and the exact dependency graph in `manager/frontend/package-lock.json`.

The GPL CAD parser sources used by the locked build are available from:

- `@mlightcad/libredwg-web` 0.7.10: <https://github.com/mlightcad/libredwg-web>
- `@mlightcad/libredwg-converter` 3.14.3: <https://github.com/mlightcad/realdwg-web>

Build the distributed frontend from the repository root with:

```bash
npm --prefix manager/frontend ci
npm --prefix manager/frontend run build
```

A distributor must keep the exact corresponding source for the binaries it
ships available under GPLv3 section 6. If upstream source availability changes,
the distributor must provide its own durable source mirror together with the
distributed build.
