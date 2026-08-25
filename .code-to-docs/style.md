# Code-to-Docs Strict Generation Rules

Follow these mandatory structural, behavioral, and depth rules when generating or updating documentation based on PR code changes:

## 1. Zero-Partial Updates Rule (Mandatory Pairings)

- **Description + Examples Pairing:** Updating a command's description paragraph WITHOUT adding/updating a corresponding CLI code example is considered a FAILED/INCOMPLETE update.
- **If you announce a new capability or flag pattern** (e.g., same-cluster/intra-cluster transfers), you MUST immediately add a matching code block under the existing examples or CLI usage section (`## Examples`, `## CLI Usage`, or equivalent). Create `## Examples` only when no suitable section already exists.

## 2. Mandatory CLI Examples Standard

- **Always Provide Concrete Code Blocks:** Every new flag, subcommand behavior, or context variation must include a copy-pasteable bash code block.
- **Use Realistic Flags:** Show exact parameter mappings instead of placeholders. For same-cluster scenarios, explicitly show matching contexts and mapped PVC names:

```bash
crane transfer-pvc \
  --source-context=mycluster --destination-context=mycluster \
  --pvc-name="mysql-data:mysql-data-new" \
  --pvc-namespace=myapp
```

## 3. End-to-End Workflow & Use-Case Integration

- **Document the Core Business Value / Use-Case:** Do not document commands in isolation. If a code change enables an overarching end-to-end workflow (e.g., StorageClass conversion using `transfer-pvc` + `export` + `transform` + `apply`), document the entire step-by-step pipeline.
- **Mandatory Placement in "Next Steps":** Always integrate multi-command workflows into the `## Next Steps` or `## Workflow` section at the bottom of the document.

## 4. Capability First, Constraints Second

- **Feature Announcement:** Always explicitly declare new capabilities and internal handling improvements first before listing restrictions or validation checks.
- **No Minimalist Notes:** Do not settle for adding only a `Note:` or warning box. Expand the main body text and examples accordingly.

## 5. Pattern Matching & Depth Alignment

- **Propose Structural Additions:** Do not restrict suggestions to small diffs. Generate complete new sections (`##`, `###`) whenever code changes introduce broad new capabilities or workflows.

## 6. Preserve Existing Document Structure

- **Do not rewrite existing paragraphs** unless the code change directly invalidates them.
- **Match the flag syntax style** of the existing file (`--flag=value`, not `--flag value`).
- **Match heading levels and formatting** (bullet style, code block language tags) exactly as they appear in the current file.
- **Minimal footprint:** touch only what the PR changes require. Do not refactor, reword, or reformat content that is unrelated to the code change.
