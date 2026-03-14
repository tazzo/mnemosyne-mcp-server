# AGENTS.md — mnemosyne-mcp-server

Go MCP (Model Context Protocol) server connecting to a pgvector Postgres DB
for semantic memory: chronicles and embeddings via Gemini.

## Build & Run

```bash
cd mnemosyne-mcp-server

# Build
go build -o bin/mnemosyne-mcp-server cmd/main.go

# Test
go test ./...

# Run in stdio mode (default, used by MCP clients)
DB_HOST=... DB_PORT=5432 DB_USER=... DB_PASS=... DB_NAME=... GEMINI_API_KEY=... \
  ./bin/mnemosyne-mcp-server

# Run as SSE server
MCP_TRANSPORT=sse PORT=8080 DB_HOST=... DB_PASS=... GEMINI_API_KEY=... \
  ./bin/mnemosyne-mcp-server
```

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `DB_HOST` | yes | Postgres host |
| `DB_PORT` | yes | Postgres port (default: 5432) |
| `DB_USER` | yes | Postgres user |
| `DB_PASS` | yes | Postgres password |
| `DB_NAME` | yes | Postgres database name |
| `GEMINI_API_KEY` | yes | Gemini API key for embeddings |
| `MCP_TRANSPORT` | no | `stdio` (default) or `sse` |
| `PORT` | no | SSE server port (default: 8080) |

## Internal Structure

```
internal/
├── db/          pgvector queries, connection pool
├── embedding/   Gemini embedding calls
├── logic/       search and ingest business logic
└── mcp/         MCP protocol handlers
```

## Conventions

- Entry point: `cmd/main.go`
- All DB operations go through `internal/db` — no direct queries outside that package
- Embeddings always via `internal/embedding` — no inline Gemini calls elsewhere
- MCP tool handlers in `internal/mcp`, delegate business logic to `internal/logic`
