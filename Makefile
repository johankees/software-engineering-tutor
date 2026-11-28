# Thin wrapper around the mise tasks in mise.toml.
MISE ?= mise

.DEFAULT_GOAL := help
.PHONY: help install serve build fmt fmt-check lint make-check go-check secrets pre-commit github-apply github-check check pr clean

help: ## List the available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

install: ## Install lint and format tools with bun
	$(MISE) run install

serve: ## Serve the site with live reload
	$(MISE) run serve

build: ## Build the site into ./site
	$(MISE) run build

fmt: ## Format Markdown, YAML and Go code
	$(MISE) run fmt

fmt-check: ## Check formatting
	$(MISE) run fmt:check

lint: ## Lint Markdown, workflows and Makefile coverage
	$(MISE) run lint

make-check: ## Check that every mise task has a Makefile target
	$(MISE) run make:check

go-check: ## Vet, lint and race-test the Go reference project
	$(MISE) run go:check

secrets: ## Scan the whole Git history for secrets
	$(MISE) run secrets

pre-commit: ## Scan staged changes for secrets
	$(MISE) run pre-commit

github-apply: ## Apply the GitHub settings and rulesets (needs repo admin)
	$(MISE) run github:apply

github-check: ## Compare the GitHub settings with this repository (read-only)
	$(MISE) run github:check

check: ## Run all checks (CI entry point)
	$(MISE) run check

pr: ## Run all checks, then ./commit.sh if present
	$(MISE) run pr

clean: ## Remove build output
	$(MISE) run clean
