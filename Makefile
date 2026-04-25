PORT       ?= 8082
ADDR        = :$(PORT)
BIN_SERVER  = bin/pprof-mcp
BIN_LOOP    = bin/loop
MCP_URL     = http://localhost:$(PORT)/mcp
MCP_NAME    = pprof-mcp
TOKEN_FILE  = .pprof-mcp-token

.PHONY: dev setup build build-server build-loop serve mcp-add mcp-remove test token

# Full dev cycle: build everything → (re)register → start server in foreground.
dev: build token mcp-add serve

setup: build token mcp-add

build: build-server build-loop

build-server:
	go build -o $(BIN_SERVER) ./cmd/pprof-mcp

build-loop:
	go build -o $(BIN_LOOP) ./cmd/loop

# Generate a stable bearer token once so `make mcp-add` (Claude registration)
# and `make serve` (server process) agree on the value across restarts. Token
# file is gitignored — rotate by deleting it and re-running `make token`.
token:
	@if [ ! -f $(TOKEN_FILE) ]; then \
		openssl rand -hex 24 > $(TOKEN_FILE); \
		chmod 600 $(TOKEN_FILE); \
		echo "generated $(TOKEN_FILE)"; \
	fi

serve: build-server token
	PPROF_MCP_TOKEN=$$(cat $(TOKEN_FILE)) ./$(BIN_SERVER) --http $(ADDR)

# Idempotent: clears any stale registration first, then adds fresh with the
# bearer header pointing at the shared token.
mcp-add: mcp-remove token
	claude mcp add --transport http --scope user $(MCP_NAME) $(MCP_URL) \
		--header "Authorization: Bearer $$(cat $(TOKEN_FILE))"

# Idempotent: succeeds whether or not the server is currently registered.
mcp-remove:
	claude mcp remove --scope user $(MCP_NAME) >/dev/null 2>&1 || true

test:
	go test ./... -count=1
