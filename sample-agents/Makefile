# Common tasks. Run `make help`. Needs: uv, azd (with `azd ext install microsoft.foundry`).
AGENTS := agents/prompt-kb agents/responses-agent agents/invocations-agent agents/dev-team
HOSTED := agents/responses-agent agents/invocations-agent agents/dev-team
SCHEMA_BASE := https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions

.PHONY: help lock test test-contract lint fmt ingest deploy refresh-schemas

help: ## Show this help
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lock: ## Regenerate every uv.lock (run after editing dependencies)
	@for d in $(AGENTS); do (cd $$d && uv lock) || exit 1; done

test-contract: ## Validate azure.yaml against the azd schemas (no Azure access)
	python -m pip install -q -r requirements-dev.txt
	python -m pytest tests -q

test: test-contract ## Run the contract tests and every agent's unit tests
	@for d in $(AGENTS); do echo "== $$d"; (cd $$d && uv run --frozen pytest -q) || exit 1; done

lint: ## Ruff and mypy on every agent
	@for d in $(AGENTS); do echo "== $$d"; (cd $$d && uv run --frozen ruff check --config ../../pyproject.toml . && uv run --frozen mypy .) || exit 1; done

fmt: ## Format all code
	@for d in $(AGENTS); do (cd $$d && uv run --frozen ruff format --config ../../pyproject.toml .) || exit 1; done

ingest: ## Upload the knowledge PDFs and save KB_VECTOR_STORE_ID in the azd environment
	@test -n "$$FOUNDRY_PROJECT_ENDPOINT" || (echo "Set FOUNDRY_PROJECT_ENDPOINT first (azd env get-values)"; exit 2)
	cd agents/prompt-kb && id=$$(uv run python ingest_knowledge.py) && azd env set KB_VECTOR_STORE_ID "$$id"

deploy: ## Provision and deploy everything
	azd up

refresh-schemas: ## Re-download the vendored azd extension schemas
	curl -fsSL $(SCHEMA_BASE)/azure.ai.agents/schemas/azure.ai.agent.json -o tests/schemas/azure.ai.agent.json
	curl -fsSL $(SCHEMA_BASE)/azure.ai.projects/schemas/azure.ai.project.json -o tests/schemas/azure.ai.project.json
	curl -fsSL $(SCHEMA_BASE)/azure.ai.connections/schemas/azure.ai.connection.json -o tests/schemas/azure.ai.connection.json
	curl -fsSL $(SCHEMA_BASE)/azure.ai.toolboxes/schemas/azure.ai.toolbox.json -o tests/schemas/azure.ai.toolbox.json
