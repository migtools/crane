# Troubleshooting and FAQ

Common issues, error messages, and solutions for `crane transform`.

## Table of Contents

- [Common Errors](#common-errors)
- [Stage Issues](#stage-issues)
- [Kustomize Issues](#kustomize-issues)
- [Resource Issues](#resource-issues)
- [Patch Issues](#patch-issues)
- [Debugging Tips](#debugging-tips)
- [FAQ](#faq)

## Common Errors

### Error: "No stages found matching selector"

**Full error:**
```
Error: no stages found matching selector
```

**Cause:** No stage directories exist in `transform/` directory.

**Solution 1:** Create default stage
```bash
crane transform  # Creates 10_KubernetesPlugin automatically
```

**Solution 2:** Create specific stage
```bash
crane transform --stage 10_KubernetesPlugin
```

**Verification:**
```bash
ls transform/
# Should show at least one stage directory
```

---

### Error: "Stage is not empty (use --force to overwrite)"

**Full error:**
```
Error: stage directory /path/to/transform/50_CustomEdits is not empty and is not a plugin stage (use --force to overwrite)
```

**Cause:** Custom stage (not ending with `Plugin`) already exists and is protected from overwrite.

**Solutions:**

**Option 1:** Use `--force` to overwrite (WARNING: loses manual edits)
```bash
crane transform --force
```

**Option 2:** Edit in place (don't re-create)
```bash
# Edit existing files
vim transform/50_CustomEdits/resources/Deployment_apps_v1_default_myapp.yaml
```

**Option 3:** Choose different stage name
```bash
crane transform --stage 60_MyNewEdits
```

**Option 4:** Commit changes first, then force
```bash
git add transform/
git commit -m "Save custom edits"
crane transform --force
git diff  # Review what changed
```

---

### Error: "Stage X requires output from stage Y, but output directory does not exist"

**Full error:**
```
Error: stage 20_OpenshiftPlugin requires output from stage 10_KubernetesPlugin, but output directory does not exist: transform/.work/10_KubernetesPlugin/output
```

**Cause:** Trying to run a later stage before earlier stages have been run.

**Solution:** Run all previous stages first
```bash
# Option 1: Run all stages
crane transform

# Option 2: Run missing predecessor explicitly
crane transform --stage 10_KubernetesPlugin
crane transform --stage 20_OpenshiftPlugin
```

**Verification:**
```bash
# Check which stages have output
ls transform/.work/*/output/
```

---

### Error: "failed to load plugins"

**Full error:**
```
Error: failed to load plugins: plugin directory does not exist: /home/user/.local/share/crane/plugins
```

**Cause:** Plugin directory doesn't exist or no plugins installed.

**Solution 1:** Check plugin directory
```bash
# Check default location
ls ~/.local/share/crane/plugins/

# Or check custom location
crane transform --plugin-dir /path/to/plugins
```

**Solution 2:** Install built-in plugins
The KubernetesPlugin is built-in and doesn't require installation. If you need additional plugins:

```bash
# Check available plugins
crane transform list-plugins

# Download additional plugins (if available)
# Consult crane-plugins repository
```

**Verification:**
```bash
crane transform list-plugins
# Should show at least KubernetesPlugin
```

---

### Error: "kubectl not found in PATH"

**Full error:**
```
Error: kubectl command not found
```

**Cause:** `kubectl` is not installed or not in PATH.

**Solution:** Install kubectl
```bash
# On Linux
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/

# On macOS
brew install kubectl

# Verify installation
kubectl version --client
```

---

## Stage Issues

### Issue: Stage has stale data

**Symptoms:**
- Resources in custom stage don't match latest transform
- Custom stage has old data from previous plugin stages

**Cause:** Plugin stages were updated, but custom stage wasn't regenerated.

**Solution:**
```bash
# Force regenerate (WARNING: loses manual edits)
crane transform --force

# Better: Commit first
git add transform/
git commit -m "Save before regeneration"
crane transform --force
git diff  # Review changes
```

**Prevention:**
Always add custom stages **last** in the pipeline.

---

### Issue: Can't create new stage between existing stages

**Scenario:**
```
Existing: 10_KubernetesPlugin, 30_CustomEdits
Want to add: 20_OpenshiftPlugin (in between)
```

**Solution:**

**Step 1:** Create the new stage
```bash
crane transform --stage 20_OpenshiftPlugin
```

**Step 2:** Regenerate later stages (WARNING: requires --force for custom stages)
```bash
crane transform --force
```

**Better approach:** Use priority spacing (10, 20, 30, ..., 90) to allow insertions.

---

### Issue: Plugin stage not regenerating

**Symptom:** Made changes to export, but plugin stage still has old data.

**Cause:** Plugin stages should auto-regenerate, but there may be an issue.

**Solution:**
```bash
# Explicitly re-run the plugin stage
crane transform --stage 10_KubernetesPlugin

# Or force all stages
crane transform --force
```

**Verification:**
```bash
# Check stage's input snapshot
ls transform/.work/10_KubernetesPlugin/input/

# Compare with export
diff -r export/resources/ transform/.work/10_KubernetesPlugin/input/
```

---

## Kustomize Issues

### Error: "kustomization.yaml validation failed"

**Symptoms:**
```
Error: error building kustomization: ...
```

**Cause:** Invalid Kustomize syntax or missing files.

**Debug:**
```bash
# Run kustomize manually to see detailed error
kubectl kustomize transform/10_KubernetesPlugin/
```

**Common issues:**

**Issue 1: Missing resource file**
```yaml
# kustomization.yaml references non-existent file
resources:
- resources/Deployment_apps_v1_default_missing.yaml  # Doesn't exist
```

**Solution:** Remove reference or create the file
```bash
# Check what files exist
ls transform/10_KubernetesPlugin/resources/

# Edit kustomization.yaml to match
vim transform/10_KubernetesPlugin/kustomization.yaml
```

**Issue 2: Invalid patch syntax**
```yaml
# patches/my-patch.yaml has syntax error
- op: add
  path: /metadata/labels/team
  # Missing 'value' field
```

**Solution:** Fix patch syntax
```bash
vim transform/10_KubernetesPlugin/patches/my-patch.yaml
```

**Issue 3: Invalid YAML**
```yaml
# Indentation error or missing colon
resources:
- resources/deployment.yaml
  - resources/service.yaml  # Wrong indentation
```

**Solution:** Fix YAML syntax
```bash
# Use a YAML validator
yamllint transform/10_KubernetesPlugin/kustomization.yaml
```

---

### Error: "patch target not found"

**Full error:**
```
Error: no matches for Id ~G_v1_ConfigMap|default|myapp-config; failed to find unique target for patch
```

**Cause:** Patch target selector doesn't match any resources.

**Debug:**
```bash
# Check what the patch targets
cat transform/10_KubernetesPlugin/kustomization.yaml | grep -A 7 "path: patches/my-patch.yaml"
```

**Example:**
```yaml
patches:
- path: patches/my-patch.yaml
  target:
    kind: ConfigMap
    name: myapp-config  # This name must match exactly
    namespace: default
```

**Solution:** Fix target selector to match resource
```bash
# Check actual resource name
grep "name:" transform/10_KubernetesPlugin/resources/ConfigMap*.yaml

# Update kustomization.yaml target
vim transform/10_KubernetesPlugin/kustomization.yaml
```

---

## Resource Issues

### Issue: Resources missing from output

**Symptom:** Expected resources don't appear in final output.

**Possible causes:**

**Cause 1: Resource was whitelisted**

Crane automatically excludes controller-managed resources (Pods, ReplicaSets, etc.).

**Check:**
```bash
# Check kustomization.yaml to see which resources are included
cat transform/10_KubernetesPlugin/kustomization.yaml | grep -A 100 "resources:"
```

**Explanation:**
Controller-managed resources (Pods, ReplicaSets, Endpoints) are not included in the `resources:` list in `kustomization.yaml` because they are created automatically by their controllers.

**Solution:** This is intentional and correct. Deploy the Deployment/StatefulSet, and controllers will recreate these resources.

**Cause 2: Resource not in kustomization.yaml**

**Check:**
```bash
grep "myresource" transform/10_KubernetesPlugin/kustomization.yaml
```

**Solution:** Add to resources list
```yaml
resources:
- resources/Deployment_apps_v1_default_myapp.yaml
- resources/Service__v1_default_myapp.yaml
- resources/ConfigMap__v1_default_myconfig.yaml  # Add this
```

**Cause 3: Resource excluded by export**

**Check:**
```bash
ls export/resources/default/ | grep MyResource
```

**Solution:** Re-export with correct filters

---

### Issue: Duplicate resources in output

**Symptom:** Same resource appears multiple times in `output/output.yaml`.

**Cause:** Resource listed multiple times in kustomization.yaml or across multiple stage resource files.

**Debug:**
```bash
# Check for duplicates in kustomization
grep "resources/" transform/10_KubernetesPlugin/kustomization.yaml | sort | uniq -d

# Check final output
kubectl kustomize transform/10_KubernetesPlugin/ | grep "^kind:"
```

**Solution:** Remove duplicates from kustomization.yaml

---

## Patch Issues

### Error: JSONPatch path escaping

**Symptom:**
```
Error: invalid patch: unable to parse path
```

**Common issue:** Forgot to escape `/` in annotation/label keys.

**Wrong:**
```yaml
- op: remove
  path: /metadata/annotations/deployment.kubernetes.io/revision
```

**Correct:**
```yaml
- op: remove
  path: /metadata/annotations/deployment.kubernetes.io~1revision
```

**Rule:** Replace `/` with `~1` in path components.

---

### Error: Patch trying to remove non-existent field

**Symptom:**
```
Error: remove operation does not apply: doc is missing path
```

**Cause:** Patch tries to remove a field that doesn't exist in the resource.

**Debug:**
```bash
# Check if field exists
kubectl kustomize transform/10_KubernetesPlugin/ | yq '.metadata.annotations'
```

**Solution 1:** Make patch conditional (not possible with JSONPatch)

**Solution 2:** Remove the patch operation

**Solution 3:** Use strategic merge patch instead
```yaml
# Use patchesStrategicMerge instead of patches
patchesStrategicMerge:
- patches/my-merge-patch.yaml
```

---

### Error: Patch array index out of bounds

**Symptom:**
```
Error: invalid patch: index out of bounds
```

**Wrong:**
```yaml
- op: add
  path: /spec/template/spec/containers/5/env  # Only 2 containers exist
  value:
    - name: MY_VAR
      value: my-value
```

**Debug:**
```bash
# Check how many containers exist
kubectl kustomize transform/10_KubernetesPlugin/ | yq '.spec.template.spec.containers | length'
```

**Solution:** Use correct index (0-based)
```yaml
- op: add
  path: /spec/template/spec/containers/0/env
  value:
    - name: MY_VAR
      value: my-value
```

---

## Debugging Tips

### Tip 1: Inspect Intermediate Output

Use `.work/` directory to debug multi-stage pipelines:

```bash
# See what Stage 1 read as input
ls transform/.work/10_KubernetesPlugin/input/

# See what Stage 1 produced (input for Stage 2)
ls transform/.work/10_KubernetesPlugin/output/

# Compare input vs output
diff -r transform/.work/10_KubernetesPlugin/input/ \
        transform/.work/10_KubernetesPlugin/output/
```

---

### Tip 2: Preview Before Applying

Always preview final output:

```bash
# Preview specific stage
kubectl kustomize transform/10_KubernetesPlugin/

# Preview final output
crane apply
cat output/output.yaml
```

---

### Tip 3: Validate Syntax

```bash
# Validate kustomization.yaml
kubectl kustomize transform/10_KubernetesPlugin/ > /dev/null

# Validate patch syntax
cat transform/10_KubernetesPlugin/patches/my-patch.yaml | jq -r '.'

# Validate YAML
yamllint transform/10_KubernetesPlugin/
```

---

### Tip 4: Use Dry-Run

```bash
# Test apply without actually applying
kubectl apply --dry-run=client -f output/output.yaml

# Server-side validation (requires cluster access)
kubectl apply --dry-run=server -f output/output.yaml
```

---

### Tip 5: Enable Debug Logging

```bash
crane transform --debug
```

This shows detailed logging about:
- Plugin execution
- Resource processing
- Patch generation
- File writes

---

### Tip 6: Compare Stages

```bash
# Compare two stages
diff <(kubectl kustomize transform/10_KubernetesPlugin/) \
     <(kubectl kustomize transform/20_OpenshiftPlugin/)

# More readable diff
diff -y <(kubectl kustomize transform/10_KubernetesPlugin/) \
        <(kubectl kustomize transform/20_OpenshiftPlugin/) | less
```

---

### Tip 7: Inspect Individual Resources

```bash
# Extract specific resource from output
kubectl kustomize transform/10_KubernetesPlugin/ | \
  yq 'select(.kind == "Deployment" and .metadata.name == "myapp")'

# Count resources by kind
kubectl kustomize transform/10_KubernetesPlugin/ | \
  yq '.kind' | sort | uniq -c
```

---

## FAQ

### Q: Why are Pods/ReplicaSets missing from the output?

**A:** These are controller-managed resources. Crane automatically excludes them by `ownerRef` because:
- Pods are created by ReplicaSets/Deployments
- ReplicaSets are created by Deployments
- Endpoints are created by Services

Deploy the controller resource (Deployment/StatefulSet/Service), and these will be recreated automatically.

**Verify:**
```bash
# Check which resources are included in the kustomization
cat transform/10_KubernetesPlugin/kustomization.yaml | grep -A 100 "resources:"
```

---

### Q: Can I run transform without exporting first?

**A:** No, `crane transform` requires input resources from `crane export` (or a previous stage).

**Workflow:**
```bash
crane export -n myapp    # Required first step
crane transform          # Requires export/ directory
```

---

### Q: How do I know which plugin a stage uses?

**A:** Stage name determines the plugin:

- `10_KubernetesPlugin` → uses `KubernetesPlugin` (default)
- `20_OpenshiftPlugin` → uses `OpenshiftPlugin`
- `50_CustomEdits` → no plugin (pass-through)

**Check available plugins:**
```bash
crane transform list-plugins
```

---

### Q: What's the difference between `crane transform` and `crane apply`?

**A:**
- **`crane transform`**: Generates transform stages (resources + patches)
- **`crane apply`**: Runs `kubectl kustomize` on stages to produce final output

**Workflow:**
```bash
crane transform  # Creates transform/ directory
crane apply      # Creates output/ directory from transform/
```

---

### Q: Can I edit resources in transform/*/resources/ directly?

**A:** 
- **Plugin stages** (ending with `Plugin`): No, changes will be overwritten
- **Custom stages** (not ending with `Plugin`): Yes, but prefer using patches

**Better approach:** Use patches instead of direct edits (easier to track in Git)

---

### Q: How do I skip a specific plugin?

**A:** Use `--skip-plugins` flag:

```bash
crane transform --skip-plugins OpenshiftPlugin,ImagestreamPlugin
```

**Check available plugins:**
```bash
crane transform list-plugins
```

---

### Q: Can I use transform output with ArgoCD/Flux directly wih kustomize?

**A:** Yes! Transform output is standard Kustomize format.

**ArgoCD example:**
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://github.com/myorg/myrepo
    path: transform/10_KubernetesPlugin
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: myapp
```

**Flux example:**
```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: myapp
spec:
  sourceRef:
    kind: GitRepository
    name: myrepo
  path: ./transform/10_KubernetesPlugin
  prune: true
```

---

### Q: What happens if I delete the .work/ directory?

**A:** It's regenerated on the next `crane transform` run. The `.work/` directory contains intermediate artifacts for debugging; it's safe to delete.

```bash
# Safe to delete
rm -rf transform/.work/

# Regenerated on next run
crane transform
```

---

### Q: How do I migrate namespaces?

**A:** Use Kustomize's `namespace` field in a custom stage:

```yaml
# transform/50_ChangeNamespace/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: new-namespace
resources:
- resources/Deployment_apps_v1_old-namespace_myapp.yaml
```

**Or use a patch:**
```yaml
- op: replace
  path: /metadata/namespace
  value: new-namespace
```

---

### Q: Can I use crane transform for non-Kubernetes resources?

**A:** No, crane transform is designed for Kubernetes manifests only. It expects:
- Valid Kubernetes YAML
- Resources with `apiVersion`, `kind`, `metadata`

---

### Q: What's the recommended Git workflow?

**A:**

**Commit:**
```gitignore
transform/*/resources/
transform/*/patches/
transform/*/kustomization.yaml
```

**Ignore:**
```gitignore
transform/.work/
output/
```

**Workflow:**
```bash
crane export -n myapp
crane transform
git add transform/
git commit -m "Add transformed manifests"
git push
```

---

### Q: How do I debug "sequential consistency" issues?

**A:** Check intermediate outputs in `.work/`:

```bash
# What did Stage 1 output?
ls transform/.work/10_KubernetesPlugin/output/

# Is it what Stage 2 expects?
ls transform/.work/20_OpenshiftPlugin/input/

# Compare
diff -r transform/.work/10_KubernetesPlugin/output/ \
        transform/.work/20_OpenshiftPlugin/input/
```

**They should be identical.**

---

## Getting Help

If you encounter issues not covered here:

1. **Check logs:**
   ```bash
   crane transform --debug
   ```

2. **Validate manually:**
   ```bash
   kubectl kustomize transform/10_KubernetesPlugin/
   ```

3. **Check GitHub issues:**
   - [Crane Issues](https://github.com/konveyor/crane/issues)

4. **File a bug report:**
   ```bash
   # Include:
   # - crane version
   # - Full error message
   # - Minimal reproduction steps
   ```

5. **Community support:**
   - Konveyor Slack
   - GitHub Discussions

---

## Summary

**Most common issues:**
- ✅ Custom stage protection → Use `--force` or edit in place
- ✅ Missing prerequisite stages → Run `crane transform` to run all
- ✅ Kustomize syntax errors → Validate with `kubectl kustomize`
- ✅ Missing resources → Check kustomization.yaml resources list
- ✅ JSONPatch escaping → Use `~1` for `/` in paths

**Best practices:**
- ✅ Always preview with `kubectl kustomize` before applying
- ✅ Use `--debug` for detailed logging
- ✅ Commit to Git before using `--force`
- ✅ Test with `kubectl apply --dry-run`
- ✅ Check `.work/` directory for debugging multi-stage issues

## Next Steps

- [**Overview**](./01-overview.md) - Understand crane transform concepts
- [**Quickstart**](./02-quickstart.md) - Hands-on tutorial
- [**Multi-Stage Pipelines**](./03-multistage.md) - Advanced workflows
