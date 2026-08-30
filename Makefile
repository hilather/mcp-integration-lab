# MCP Integration Lab — thin wrappers over the mcplab CLI (cmd/mcplab).
# Select a profile with: make up PROFILE=<name>   (directories under profiles/)
SHELL := /bin/bash
export PROFILE

.PHONY: help up down reset register preflight smoke vendor secrets creds fixtures reload labldap-up labldap-down labtacacs-up labtacacs-down test

help:
	@go run ./cmd/mcplab 2>&1 || true

up:
	go run ./cmd/mcplab up

down:
	go run ./cmd/mcplab down

reset:
	go run ./cmd/mcplab reset

register:
	go run ./cmd/mcplab register

preflight:
	go run ./cmd/mcplab preflight

smoke:
	go run ./cmd/mcplab smoke

vendor:
	go run ./cmd/mcplab vendor

secrets:
	go run ./cmd/mcplab secrets

creds:
	go run ./cmd/mcplab creds

fixtures:
	go run ./cmd/mcplab fixtures

# Recreate one app. APP=labdns|maildev|nfs|labinfo|mcpjungle|labldap|labtacacs|labmitm|labgraph
reload:
	@test -n "$(APP)" || { echo "usage: make reload APP=labdns|maildev|nfs|labinfo|mcpjungle|labldap|labtacacs|labmitm|labgraph" >&2; exit 2; }
	go run ./cmd/mcplab reload $(APP)

labldap-up:
	go run ./cmd/mcplab labldap-up

labldap-down:
	go run ./cmd/mcplab labldap-down

labtacacs-up:
	go run ./cmd/mcplab labtacacs-up

labtacacs-down:
	go run ./cmd/mcplab labtacacs-down

# Unit + regression tests for the orchestration CLI. Run before landing
# changes to cmd/, internal/, compose files, or profiles.
test:
	go vet ./...
	go test ./...
