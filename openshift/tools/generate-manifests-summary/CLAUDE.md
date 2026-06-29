# generate-manifests-summary Tool

## Purpose

A standalone Go CLI tool that parses multi-document Kubernetes manifest YAML files
and generates a structured summary organized by profile. Used in the OpenShift
CAPI provider build pipeline to create manifest inventories.

## What It Does

Reads a multi-document YAML file (e.g., `manifests.yaml` containing multiple
`---`-separated Kubernetes resources), extracts metadata from each resource
(kind, apiVersion, name, namespace), and writes a profile-organized summary to
`manifests-summary.yaml`.

The tool:
- Parses using `k8s.io/apimachinery/pkg/util/yaml` (handles multi-doc YAML/JSON)
- Extracts only metadata (no spec or status fields)
- Groups manifests by profile name (e.g., "default")
- Sorts manifests lexicographically (kind → apiVersion → namespace → name)
- Preserves existing profiles when updating (merges, doesn't overwrite entire file)
- Uses `sigs.k8s.io/yaml` for consistent Kubernetes-style YAML output

## Usage

```bash
go run main.go \
  --manifests-file=capi-operator-manifests/default/manifests.yaml \
  --manifests-summary-file=manifests-summary.yaml \
  --profile=default
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--manifests-file` | `capi-operator-manifests/default/manifests.yaml` | Input multi-doc YAML |
| `--manifests-summary-file` | `manifests-summary.yaml` | Output summary file |
| `--profile` | `default` | Profile name for grouping manifests |

## Output Format

```yaml
default:
  - kind: CustomResourceDefinition
    apiVersion: apiextensions.k8s.io/v1
    name: awsclusters.infrastructure.cluster.x-k8s.io
    namespace: ""
  - kind: Deployment
    apiVersion: apps/v1
    name: capa-controller-manager
    namespace: capa-system
```

Each profile is a top-level key. Manifests under that profile are sorted and
deduplicated.

## Code Structure

| Function | Responsibility |
|----------|----------------|
| `main()` | CLI entry point, flag parsing |
| `run()` | Orchestrates read → parse → update → write flow |
| `parseManifests()` | YAML decoder loop, returns `[]ManifestMetadata` |
| `parseDocument()` | Validates single doc, extracts metadata |
| `readExistingSummary()` | Loads existing summary file (if present) |
| `updateSummary()` | Merges new manifests into profile, sorts |
| `writeSummary()` | Marshals and writes YAML output |
| `parseAPIVersion()` | Utility to split `apiVersion` into group/version (currently unused) |

## Key Implementation Details

- **Multi-document parsing**: Uses `yaml.NewYAMLOrJSONDecoder` with `io.EOF` loop
  termination (standard k8s pattern).
- **Validation**: Requires `kind` and `metadata.name` (empty values → error).
- **Merge behavior**: Reads existing summary, updates only the specified profile,
  leaves other profiles untouched.
- **Sorting**: 4-level sort (kind → apiVersion → namespace → name) ensures stable,
  reviewable diffs.
- **Logging**: Uses `klog` with `-v=1` for debug output.

## Dependencies

```go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/util/yaml"
    "k8s.io/klog/v2"
    sigsyaml "sigs.k8s.io/yaml"
)
```

All dependencies are from `k8s.io/apimachinery` (parsing, types) and
`sigs.k8s.io/yaml` (output marshaling). No controller-runtime or CAPI imports.

## Agent Instructions

### Before Making Changes

- This is a **standalone tool**, not part of the main CAPI provider controller logic.
- Changes here do **not** require `make generate` in the parent repo.
- The tool has **no unit tests yet**. If adding functionality, add tests in `main_test.go`.
- Do not add dependencies beyond `k8s.io/apimachinery` and `sigs.k8s.io/yaml`
  unless absolutely necessary.

### Code Modifications

- **Adding fields to `ManifestMetadata`**: Update the struct, then update
  `parseDocument()` to populate the new field.
- **Changing sort order**: Modify the `sort.Slice` comparator in `updateSummary()`.
- **Adding validation**: Extend `parseDocument()` with new checks.
- **Changing output format**: Modify `writeSummary()` or introduce a new marshaling function.

### Testing Changes Manually

```bash
# Generate a test manifest file
cat > test-manifests.yaml <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deploy
  namespace: test-ns
EOF

# Run the tool
go run main.go \
  --manifests-file=test-manifests.yaml \
  --manifests-summary-file=test-summary.yaml \
  --profile=test

# Verify output
cat test-summary.yaml
```

### Common Modifications

**Add a new flag**:
1. Add the flag variable in `main()` (e.g., `sortOrder := flag.String(...)`).
2. Pass it to `run()`.
3. Use it in the appropriate function (`updateSummary()`, `writeSummary()`, etc.).

**Change validation rules**:
1. Edit `parseDocument()`.
2. Add new error returns for invalid cases.

**Add a new output field** (e.g., labels):
1. Add to `ManifestMetadata` struct.
2. Update `parseDocument()` to extract it from `obj.Labels`.
3. YAML marshaling is automatic via struct tags.

### Build & Install

This tool is not installed by the main repo Makefile. To build:

```bash
cd openshift/tools/generate-manifests-summary
go build -o generate-manifests-summary .
```

Or run directly:

```bash
go run main.go [flags]
```

## Related Files

- **Parent repo Makefile**: May invoke this tool as part of OpenShift-specific
  build targets (check for `manifests-summary` targets).
- **Input manifests**: Typically generated by `kustomize build` or similar.
- **Output summary**: Used for inventory tracking, diffing, or CI validation.

## Notes for AI Agents

- **Scope**: This is a parsing/transformation utility, not a controller or API definition.
- **No API changes**: This tool does not define or modify CRDs.
- **No code generation**: This tool is not invoked by `make generate`.
- **Standalone testing**: Test manually with sample YAML files; add unit tests if
  extending functionality.
- **Error handling**: All errors are surfaced via `klog.Fatal` (CLI tool pattern,
  not controller pattern).
