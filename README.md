# docod
Sync your docs as fast as you ship your code.

`docod` is an AI-assisted documentation agent for Go codebases. It scans a
project, stores a local knowledge graph in SQLite, and updates generated
documentation from `docs/doc_model.json`.

## Getting Started

Build the CLI from source:

```bash
make build
```

The binary is written to `bin/docod`. You can also run it directly with Go:

```bash
go run ./cmd/docod --help
```

## Common Commands

```bash
# Scan the current project into the local knowledge graph
bin/docod scan .

# Bootstrap or incrementally update generated documentation
bin/docod sync

# Force a sync even when git reports no changes
bin/docod sync --force
```

By default, `docod` reads `config.yaml` and stores local state in `docod.db`.
Set `DOCOD_EMBEDDING_API_KEY` and `DOCOD_LLM_API_KEY` when using providers that
require API keys.

## Development

```bash
make test
make fmt
```
