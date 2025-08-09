# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common Commands

### Build & Development

- `task build` - Build the binary to `bin/nitroso-zinc`
- `task setup` - Setup the repository (runs secrets script and `go mod tidy`)
- `task run -- <command>` - Run the application with secrets loaded
- `task dev -- <command>` - Run application with infrastructure (development mode)
- `task dev:watch -- <command>` - Run application with infrastructure and hot reloading using air

### Other Utilities

- `task sdk:gen` - Generate SDK from OpenAPI specs
- `task process-proxy -- <args>` - Process proxy configurations from webshare
- `task update-proxy` - Update proxy configurations from webshare
- `task latest` - Get latest versions of OCI dependencies

### Infrastructure

- `task helm:*` - Helm chart operations (see Taskfile.helm.yml)
- `task tear:*` - Infrastructure teardown operations
- `task stop:*` - Stop running services

## Architecture Overview

**Platform**: Nitroso Tin is a multi-component Go application for train ticket booking automation on the KTMB system.

### Core Components (CLI Commands)

- **CDC** (`cdc`) - Change data capture component
- **Poller** (`poller`) - Kubernetes job polling system for ticket availability
- **Enricher** (`enricher`) - Enriches ticket data with additional information
- **Reserver** (`reserver`) - Handles ticket reservation logic
- **Buyer** (`buyer`) - Executes actual ticket purchases
- **Terminator** (`terminator`) - Terminates and cleans up processes

### Key Libraries & Systems

- **Authentication**: Uses Descope M2M credentials via `lib/auth/credentials_provider.go`
- **Configuration**: Hierarchical config system with landscape-specific settings in `system/config/`
- **Telemetry**: Full OpenTelemetry setup with metrics, traces, and logging via `system/telemetry/`
- **Redis Integration**: Stream processing and caching via `lib/otelredis/`
- **KTMB Integration**: HTTP client for KTMB API in `lib/ktmb/`
- **Encryption**: Symmetric encryption for sensitive data in `lib/encryptor/`

### Configuration Management

- Landscape-based configuration (lapras, pichu, pikachu, raichu)
- Base config in `config/app/` with landscape-specific overrides
- Environment variables: `LANDSCAPE` (default: lapras), `BASE_CONFIG` (default: ./config/app)

### Infrastructure

- Kubernetes deployments via Helm charts in `infra/`
- Docker images built for both AMD64 and ARM64
- Tilt configuration for local development in `Tiltfile` and `config/tilt/`
- Nix flake for reproducible development environment

### Development Environment

- Uses Nix flakes for consistent development setup
- Pre-commit hooks configured via `nix/pre-commit.nix`
- Go 1.21.4 with extensive OpenTelemetry instrumentation

## Key Development Notes

- The application runs as a CLI with multiple subcommands corresponding to different components
- State is shared across commands via the `State` struct in `cmds/state.go`
- Configuration is loaded at startup and passed to all components
- All components include comprehensive telemetry and logging
- Redis is used for both caching and stream processing
- The system is designed to run in Kubernetes with job-based polling patterns
