# AGENTS.md

This file provides guidance to AI agents when working with code in this repository.

## Repository Overview

`gcp-hcp-ctl` is a Go CLI for managing GCP Hosted Control Plane (HCP) clusters. Cluster and nodepool lifecycle commands use the Platform API. Operational debugging commands use Cloud Workflows to reach GKE clusters without direct operator access (Zero Operator Access pattern).

**Module path**: `github.com/openshift-online/gcp-hcp-ctl`

## Development Commands

```bash
make build    # Build bin/gcphcpctl
make test     # Run unit tests with race detection
make lint     # Run go vet
make clean    # Remove build artifacts
```

## Project Structure

```text
cmd/
├── gcphcpctl/        # Main CLI entry point
└── ops/              # Standalone plugin entry point (future extraction)
pkg/
├── cli/              # Root command, version, completion
├── config/           # Config file loading (~/.gcphcpctl/config.yaml)
├── cluster/          # Cluster lifecycle commands
├── nodepool/         # Nodepool lifecycle commands
├── auth/             # Identity-token acquisition
├── platformapi/      # Shared Platform API client and error normalization
├── infra/            # IAM and network infrastructure orchestration
├── ops/              # Operational commands (self-contained, extractable)
│   ├── companion/    # AI companion (PagerDuty, tools, sessions)
│   ├── pam/          # Privileged Access Manager commands
│   └── wf/           # Cloud Workflow management subcommands
├── gcp/
│   ├── auditlog/     # Cloud Audit Log client
│   ├── cloudrun/     # Cloud Run client
│   ├── iam/          # GCP IAM client
│   ├── networking/   # GCP networking client
│   ├── pam/          # PAM API client
│   └── workflows/    # Cloud Workflows API client + callbacks
└── output/           # Table and JSON output formatting
hack/workflows/       # Cloud Workflow YAML definitions
```

## Architecture

The `ops` subtree is self-contained under `pkg/ops/` with no dependencies on `pkg/cli/`. This allows extraction into a standalone plugin binary (`gcphcpctl-ops`). A stub entry point exists at `cmd/ops/main.go`.

Cluster and nodepool lifecycle requests go through the Platform API; `ops` debugging and remediation use Cloud Workflows (Zero Operator Access). Workflows are deployed to the management cluster's GCP project and use the GKE API with Workload Identity. Normalize Platform API errors only in `pkg/platformapi`: display fixed labels, never server prose, and do not infer policy causes from a 403.

## Code Conventions

- Go 1.25+ required
- Google Cloud API clients use ADC; local user ADC can be set up with `gcloud auth application-default login`
- Platform API identity tokens use service-account ADC or external-account ADC with service-account impersonation; user ADC or absent ADC selects the active gcloud session, which needs `gcloud auth login` (see `pkg/auth/auth.go`)
- Configuration priority: CLI flags > environment variables > config file (`~/.gcphcpctl/config.yaml`)
- Version info injected via `-ldflags` at build time (see `Makefile`)

## Testing

```bash
make test     # Runs go test -race ./...
```

Tests use standard Go testing with table-driven patterns. Mock GCP clients are used for unit tests.

## Security

- No hardcoded credentials; Platform API authentication uses ADC or gcloud fallback, and workflows use Workload Identity
- Cloud Workflows provide an auditable, controlled access layer for operational cluster debugging
- PAM integration enforces just-in-time privileged access
