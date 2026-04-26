# Troubleshooting

Common issues and solutions when using Crane.

## Export Issues

### No resources exported

**Symptoms:** `export/resources/<namespace>/` is empty or missing.

**Solutions:**
- Verify the namespace exists: `kubectl get ns <namespace>`
- Check RBAC permissions: `kubectl auth can-i list deployments -n <namespace>`
- Try with a specific label selector to narrow scope: `crane export -n <ns> -l app=myapp`

### "namespace must be set"

**Cause:** No namespace specified and none in current kubeconfig context.

**Fix:** Use `-n <namespace>` or set a default namespace in your kubeconfig context.

### Export failures directory has errors

**Cause:** Some resources couldn't be listed (usually RBAC or CRD issues).

**Action:** Review files in `export/failures/<namespace>/`. Common causes:
- Missing CRDs on the cluster
- Insufficient permissions for certain resource types
- API server rate limiting (increase `--qps` and `--burst`)

### API throttling

**Symptoms:** Slow export, timeout errors.

**Fix:** Adjust rate limits:
```bash
crane export -n my-app --qps 200 --burst 2000
```

## Transform Issues

### "stage directory is not empty (use --force to overwrite)"

**Cause:** A custom stage (not ending with `Plugin`) already has content.

**Solutions:**
```bash
# Force overwrite (WARNING: deletes manual edits in the stage)
crane transform --force

# Or only re-run plugin stages (they auto-regenerate)
crane transform --stage 10_KubernetesPlugin
```

### "stage requires plugin 'X' but it was not found"

**Cause:** Stage name ends with `Plugin` but no matching plugin binary exists.

**Solutions:**
- List available plugins: `crane transform list-plugins`
- Install the missing plugin to `~/.local/share/crane/plugins/`
- Or use a custom stage name (without `Plugin` suffix) for manual editing

### Plugin conflicts

**Symptoms:** `ignored-patches-report.yaml` shows discarded patches.

**Action:** Review the report. If needed, adjust plugin order or skip conflicting plugins:
```bash
crane transform --skip-plugins conflicting-plugin
```

## Apply Issues

### "kubectl not found"

**Cause:** `kubectl` is not installed or not in PATH.

**Fix:** Install kubectl and verify: `kubectl version --client`

### "kustomization.yaml validation failed"

**Cause:** Invalid Kustomize syntax or missing resource/patch files.

**Debug:**
```bash
# Validate manually to see detailed error
kubectl kustomize transform/10_KubernetesPlugin/

# Check for missing files
ls transform/10_KubernetesPlugin/resources/
ls transform/10_KubernetesPlugin/patches/
```

### Resources missing from output

**Debug:**
```bash
# Check intermediate artifacts
ls transform/.work/10_KubernetesPlugin/input/
ls transform/.work/10_KubernetesPlugin/output/

# Compare input vs output
diff -r transform/.work/10_KubernetesPlugin/input/ transform/.work/10_KubernetesPlugin/output/

# Run with debug logging
crane transform --debug
```

## Validate Issues

### Validation reports incompatible resources

**Cause:** Target cluster doesn't serve the required API versions/kinds.

**Solutions:**
- Install required CRDs on the target cluster
- Install required operators
- Add a transform stage to convert resources to supported API versions
- Review `validate/failures/` for details

### "input-dir is not a directory"

**Cause:** `crane apply` hasn't been run yet.

**Fix:** Run `crane apply` first to generate the output directory.

## PVC Transfer Issues

### Transfer hangs at "waiting for endpoint"

**Cause:** The endpoint (Route or Ingress) isn't becoming healthy.

**Solutions:**
- **Route endpoint:** Verify the destination is an OpenShift cluster
- **nginx-ingress endpoint:** Verify nginx ingress controller is running:
  ```bash
  kubectl --context dest get pods -n ingress-nginx
  ```
- Check network connectivity between clusters
- Verify DNS resolution for the subdomain (nginx-ingress)

### "subdomain cannot be empty when using nginx ingress"

**Fix:** Provide `--subdomain` and `--ingress-class`:
```bash
crane transfer-pvc --endpoint nginx-ingress --subdomain transfer.example.com --ingress-class nginx ...
```

### "both source and destination cluster are the same"

**Cause:** Source and destination contexts point to the same cluster.

**Fix:** Use different kubeconfig contexts for source and destination.

### Transfer is slow

**Potential causes:**
- Network bandwidth between clusters
- Large volume size
- Checksum verification enabled (`--verify`)

**Mitigations:**
- Run transfers in parallel for independent PVCs
- Ensure adequate network bandwidth
- Use `--verify` only for the final transfer pass

## General Tips

### Enable Debug Logging

Most commands support `--debug` for verbose output:

```bash
crane export --debug -n my-app
crane transform --debug
```

### Check Crane Version

```bash
crane version
```

### Reset and Start Over

If the migration state becomes inconsistent:

```bash
# Remove all generated directories and start fresh
rm -rf export/ transform/ output/ validate/
crane export -n my-app
crane transform
crane apply
```
