# AGENTS.md

This file provides guidance to coding agents (e.g. Claude Code, claude.ai/code) when working with code in this repository.

## Repository purpose

Go module `github.com/kluster-manager/cluster-auth` — an OCM (Open Cluster Management) **addon** that distributes RBAC across managed clusters. The hub-side manager defines new cross-cluster RBAC types (`ManagedClusterRole`, `ManagedClusterRoleBinding`, `ManagedClusterSetRoleBinding`) plus account types (`Account`); the agent on each managed cluster materializes them as native Kubernetes `Role`/`RoleBinding` objects.

The produced binary is `cluster-auth`. It exposes two subcommands — `manager` (runs on the OCM hub) and `agent` (runs on each managed cluster). The README is a Kubebuilder scaffold stub; treat this file as the source of truth.

Note on naming: the local filesystem path is `open-cluster-management.io/cluster-auth` (so imports look like vanity URLs), but the **actual Go module is `github.com/kluster-manager/cluster-auth`** and the binary uses GitHub-org imports throughout.

## Architecture

- `cmd/cluster-auth/main.go` — entry point; builds a Cobra root command and runs.
- `pkg/cmds/`:
  - `root.go` — top-level command.
  - `manager/` — `manager` subcommand (hub-side).
  - `agent/` — `agent` subcommand (spoke-side).
- `apis/` — two API groups (`API_GROUPS := authentication:v1alpha1 authorization:v1alpha1`):
  - `apis/authentication/v1alpha1/` — `Account` and supporting types.
  - `apis/authorization/v1alpha1/` — `ManagedClusterRole`, `ManagedClusterRoleBinding`, `ManagedClusterSetRoleBinding`.
- `crds/` — generated CRD YAMLs (`authentication.k8s.appscode.com_accounts.yaml`, `authorization.k8s.appscode.com_managedclusterroles.yaml`, etc.).
- `pkg/manager/` — hub-side:
  - `addon.go` — `ManagedClusterAddOn` configuration; embeds `agent-manifests/` via `go:embed all:agent-manifests` and renders values for each managed cluster via `open-cluster-management.io/addon-framework`.
  - `controller/authentication/` and `controller/authorization/` — controllers for the four hub-side CRDs.
  - `rbac/` — RBAC manifest generation helpers.
  - `agent-manifests/cluster-auth/` — embedded chart/manifests that get pushed down to each managed cluster.
- `pkg/agent/controller/` — spoke-side controllers:
  - `managedclusterrole_controller.go`, `managedclusterrolebinding_controller.go` — translate the OCM CRs into native `Role`/`RoleBinding`.
- `pkg/common/constants.go` — shared annotation/label/finalizer strings.
- `pkg/utils/` — helpers.
- `Dockerfile.in` (PROD, distroless) + `Dockerfile.dbg` (debian) — two image variants (no UBI for this one).
- `hack/`, `Makefile` — AppsCode build harness (runs everything inside `ghcr.io/appscode/golang-dev`).
- `vendor/` — checked-in deps.

The binary requires a valid AppsCode license at runtime — `main.go` blank-imports `go.bytebuilders.dev/license-verifier/info`.

## Common commands

All Make targets run inside `ghcr.io/appscode/golang-dev` — Docker must be running.

- `make build` / `make all-build` — build host or all-platform binaries.
- `make gen` — regenerate clientset + manifests. Run after any change to `apis/**/*_types.go`.
- `make manifests` — regenerate CRDs only.
- `make clientset` — regenerate client code.
- `make openapi` — regenerate OpenAPI definitions (currently commented out of `gen`).
- `make fmt` — gofmt + goimports.
- `make lint` — golangci-lint.
- `make unit-tests` / `make test` — Go unit tests.
- `make verify` — `verify-gen verify-modules`; `go mod tidy && go mod vendor` must leave the tree clean.
- `make container` — build PROD and DBG images.
- `make push` — push both image variants; `make docker-manifest` writes multi-arch manifests; `make release` is the full publish flow.
- `make push-to-kind` / `make deploy-to-kind` — load into Kind and Helm-install.
- `make install` / `make uninstall` / `make purge` — Helm install lifecycle.
- `make add-license` / `make check-license` — manage license headers.

Run a single Go test (requires a local Go toolchain):

```
go test ./pkg/manager/controller/... -run TestName -v
```

## Conventions

- Module path is `github.com/kluster-manager/cluster-auth` (the **GitHub URL is canonical** — there is no vanity-URL redirect for this repo). The local filesystem path under `open-cluster-management.io/` is for layout convenience only; imports use the GitHub path.
- The CRD API groups are `authentication.k8s.appscode.com` and `authorization.k8s.appscode.com` (note the `.k8s.appscode.com` domain — these are AppsCode-specific extensions to OCM, not upstream OCM types).
- License: Apache-2.0 (`LICENSE`). New files need the standard "Copyright AppsCode Inc. and Contributors." header (`make add-license`).
- Sign off commits (`git commit -s`); contributions follow the DCO (`DCO` file).
- Vendor directory is checked in — `go mod tidy && go mod vendor` must leave the tree clean (enforced by `verify-modules`).
- Binary requires a valid AppsCode license at runtime (license-verifier blank import).
- Do not hand-edit `zz_generated.*.go` or anything under `crds/` — change `apis/**/*_types.go` and re-run `make gen`.
- The hub-side addon definition (`pkg/manager/addon.go`) renders `agent-manifests/cluster-auth/values.yaml` per managed cluster; keep that path stable — the embed directive `//go:embed all:agent-manifests` depends on it.
- Two Dockerfiles, one binary — keep `Dockerfile.in` and `Dockerfile.dbg` in sync when changing build steps.
