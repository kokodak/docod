# Incremental Documentation Model Plan

## Goal

Treat JSON as the source of truth for document structure and content.
Generate Markdown as a derived artifact.
Bias output toward official technical docs:
- semantic explanations (intent, behavior, constraints)
- conceptual diagrams (Mermaid) where helpful
- task-oriented examples
- avoid call-graph narration ("used by", "called from")

## Files

- `docs/doc_model.json` (versioned state, committed to git)
- `docs/doc_model.schema.json` (validation contract)
- `docs/documentation.md` (derived output)

## State Strategy

- Keep `doc_model.json` in the repository for reviewable diffs.
- Keep vector/graph caches out of `main` branch history.
- Use SQLite only as optional ephemeral cache (local or CI workspace), not as truth source.

## Incremental Update Flow

1. Parse PR diff and extract changed symbols.
2. Expand impact set via graph traversal.
3. Map impacted symbols to section `sources`.
4. Patch only impacted sections in `doc_model.json`.
5. Validate model:
   - required sections exist
   - source references are valid
   - section ordering/tree integrity
6. Regenerate `docs/documentation.md` from the model.
7. Commit model + markdown changes together.
