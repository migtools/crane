# Instructions File — E2E Test Cases

The `--instructions-file` flag for `crane transform` allows users to define the transform pipeline stages, their order, and per-stage optional flags in a declarative YAML file instead of CLI arguments.

## Instructions File Format

```yaml
# Simple format — just stage names
stages:
  - KubernetesPlugin

# Object format — stage names with per-stage optionals
stages:
  - name: KubernetesPlugin
    optionals:
      pvc-rename-map: "old-pvc:new-pvc"
      registry-replacement: "docker.io=quay.io"

# Mixed format — strings and objects together
stages:
  - KubernetesPlugin
  - name: CustomPlugin
    optionals:
      my-flag: "value"
```

## Test Cases

### TC-1: Basic single-stage instructions file

**What:** Run transform with an instructions file containing a single `KubernetesPlugin` stage.

**Steps:**
1. Deploy a simple app (configmap + deployment) on source
2. `crane export`
3. Create instructions file with `stages: [KubernetesPlugin]`
4. `crane transform --instructions-file instructions.yaml`
5. `crane apply`
6. Apply to target and validate

**Verify:**
- Transform creates `10_KubernetesPlugin` stage directory
- KubernetesPlugin patches are generated (server-managed metadata stripped)
- Output matches running without instructions file

**Priority:** Tier 0

---

### TC-2: Instructions file with pvc-rename-map optionals

**What:** Use instructions file to pass `pvc-rename-map` as a per-stage optional instead of `--optional-flags` JSON.

**Steps:**
1. Deploy MongoDB with PVC `mongodb-data`
2. `crane export`
3. Create instructions file:
   ```yaml
   stages:
     - name: KubernetesPlugin
       optionals:
         pvc-rename-map: "mongodb-data:mongodb-data-new"
   ```
4. `crane transform --instructions-file instructions.yaml`
5. `crane apply`
6. Verify output.yaml has `claimName: mongodb-data-new`

**Verify:**
- The Deployment's volume claimName is rewritten to `mongodb-data-new`
- Equivalent to `--optional-flags '{"pvc-rename-map":"mongodb-data:mongodb-data-new"}'`

**Priority:** Tier 0

---

### TC-3: Instructions file with registry-replacement

**What:** Use instructions file to replace container image registries.

**Steps:**
1. Deploy app with image `docker.io/library/nginx:latest`
2. `crane export`
3. Create instructions file:
   ```yaml
   stages:
     - name: KubernetesPlugin
       optionals:
         registry-replacement: "docker.io=quay.io"
   ```
4. `crane transform --instructions-file instructions.yaml`
5. `crane apply`
6. Verify output.yaml has `quay.io/library/nginx:latest`

**Verify:**
- All container image references are rewritten from `docker.io` to `quay.io`

**Priority:** Tier 1

---

### TC-4: Instructions file with multiple optionals

**What:** Combine pvc-rename-map and registry-replacement in a single instructions file.

**Steps:**
1. Deploy MongoDB with PVC and `docker.io` image
2. `crane export`
3. Create instructions file:
   ```yaml
   stages:
     - name: KubernetesPlugin
       optionals:
         pvc-rename-map: "mongodb-data:mongodb-data-new"
         registry-replacement: "docker.io=quay.io"
   ```
4. `crane transform --instructions-file instructions.yaml`
5. `crane apply`
6. Verify both transformations applied

**Verify:**
- PVC reference rewritten AND image registry replaced in the same transform

**Priority:** Tier 1

---

### TC-5: Instructions file mutual exclusion with --optional-flags

**What:** Verify that `--instructions-file` cannot be used together with `--stage-optionals`.

**Steps:**
1. Create any instructions file
2. Run `crane transform --instructions-file instructions.yaml --stage-optionals 'KubernetesPlugin={"key":"val"}'`

**Verify:**
- Command fails with: `use either --instructions-file or --stage-optionals, not both`

**Priority:** Tier 0

---

### TC-6: Instructions file mutual exclusion with positional stage arguments

**What:** Verify that `--instructions-file` cannot be used together with positional stage arguments.

**Steps:**
1. Create any instructions file
2. Run `crane transform --instructions-file instructions.yaml KubernetesPlugin`

**Verify:**
- Command fails with: `use either --instructions-file or positional stage arguments, not both`

**Priority:** Tier 0

---

### TC-7: Invalid instructions file — empty stages

**What:** Instructions file with no stages should fail validation.

**Steps:**
1. Create instructions file with `stages: []`
2. Run `crane transform --instructions-file instructions.yaml`

**Verify:**
- Command fails with: `instructions file must contain at least one stage`

**Priority:** Tier 0

---

### TC-8: Invalid instructions file — duplicate stages

**What:** Instructions file with duplicate stage names should fail.

**Steps:**
1. Create instructions file:
   ```yaml
   stages:
     - KubernetesPlugin
     - KubernetesPlugin
   ```
2. Run `crane transform --instructions-file instructions.yaml`

**Verify:**
- Command fails with: `duplicate stage "KubernetesPlugin"`

**Priority:** Tier 0

---

### TC-9: Invalid instructions file — unknown top-level key

**What:** Instructions file with unknown fields should fail with a friendly error.

**Steps:**
1. Create instructions file:
   ```yaml
   stages:
     - KubernetesPlugin
   plugins:
     - SomePlugin
   ```
2. Run `crane transform --instructions-file instructions.yaml`

**Verify:**
- Command fails with: `unknown field "plugins" (supported top-level keys: stages)`

**Priority:** Tier 0

---

### TC-10: Invalid instructions file — unknown stage field

**What:** Stage entry with unknown field should fail.

**Steps:**
1. Create instructions file:
   ```yaml
   stages:
     - name: KubernetesPlugin
       priority: 10
   ```
2. Run `crane transform --instructions-file instructions.yaml`

**Verify:**
- Command fails with: `unknown field "priority" (supported fields: name, optionals)`

**Priority:** Tier 0

---

### TC-11: Instructions file — mixed string and object format

**What:** Verify mixed-format stages list (backward compatible strings + objects with optionals).

**Steps:**
1. Deploy app with PVC
2. `crane export`
3. Create instructions file:
   ```yaml
   stages:
     - KubernetesPlugin
   ```
4. `crane transform --instructions-file instructions.yaml`
5. `crane apply`
6. Verify output is correct

**Verify:**
- String entries work alongside object entries
- Stage directories created with correct numeric prefixes

**Priority:** Tier 1

---

### TC-12: Instructions file — multi-document YAML rejected

**What:** YAML with multiple documents (`---` separator) should be rejected.

**Steps:**
1. Create instructions file:
   ```yaml
   stages:
     - KubernetesPlugin
   ---
   stages:
     - AnotherPlugin
   ```
2. Run `crane transform --instructions-file instructions.yaml`

**Verify:**
- Command fails with: `only a single YAML document is allowed`

**Priority:** Tier 1

---

### TC-13: Instructions file — nonexistent file

**What:** Pointing to a file that doesn't exist should fail clearly.

**Steps:**
1. Run `crane transform --instructions-file /nonexistent/path.yaml`

**Verify:**
- Command fails with: `failed to read instructions file`

**Priority:** Tier 0

---

### TC-14: Instructions file equivalence with CLI flags

**What:** Verify that instructions file produces identical output to equivalent CLI flags.

**Steps:**
1. Deploy app with PVC
2. `crane export`
3. Run transform two ways:
   - `crane transform --optional-flags '{"pvc-rename-map":"old:new"}'`
   - `crane transform --instructions-file instructions.yaml` (with equivalent content)
4. Compare both outputs

**Verify:**
- Both produce identical `output.yaml` after `crane apply`

**Priority:** Tier 1

---

### TC-15: Full e2e — StorageClass conversion via instructions file

**What:** Complete SC conversion workflow using instructions file instead of CLI flags.

**Steps:**
1. Deploy MongoDB, seed data
2. Scale down, transfer PVC with `--dest-storage-class`
3. `crane export`
4. Create instructions file with `pvc-rename-map`
5. `crane transform --instructions-file instructions.yaml`
6. `crane apply`
7. Apply to target, scale up, verify data

**Verify:**
- End-to-end SC conversion works with instructions file
- MongoDB data intact on target

**Priority:** Tier 0

---

## Summary

| Priority | Count | Coverage |
|----------|-------|----------|
| Tier 0 | 8 | Basic flow, validation errors, mutual exclusion, full e2e |
| Tier 1 | 7 | Registry replacement, mixed format, equivalence, edge cases |
| **Total** | **15** | |

## Notes

- TC-7 through TC-13 (validation tests) can be unit tests — they don't need clusters
- TC-1, TC-2, TC-14, TC-15 need cluster access (e2e)
- TC-5, TC-6 can be either unit or e2e (just check error message)
- Many validation cases are already covered by existing unit tests in `instructions_test.go` — the e2e tests should focus on the end-to-end flow (TC-1, TC-2, TC-14, TC-15)
