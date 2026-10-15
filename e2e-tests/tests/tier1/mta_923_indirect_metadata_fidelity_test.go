package e2e

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// permFidelityDir is seeded under the app's /test-data PVC with a matrix of files
// and directories whose POSIX permission bits and ownership are captured before and
// after an indirect transfer.
const permFidelityDir = "/test-data/perm-fidelity"

// plainRegularFiles are the entries (relative to permFidelityDir, "./"-prefixed to
// match `find`'s output) whose permission-bit fidelity crane currently guarantees:
// regular files with ordinary rwx modes and no special bits.
var plainRegularFiles = []string{
	"./file_400", "./file_444", "./file_600", "./file_640", "./file_644",
	"./file_664", "./file_700", "./file_750", "./file_755",
	"./nested/deep/file_640",
}

// statEntry is one row of a stat manifest: the POSIX mode (octal, left-padded to
// 4 digits so special bits compare cleanly) plus numeric owner uid and gid.
type statEntry struct {
	mode string
	uid  string
	gid  string
}

// padMode left-pads an octal mode to 4 digits so "644" and "0644" compare equal
// and special-bit modes like "4755" line up against three-digit plain modes.
func padMode(m string) string {
	m = strings.TrimSpace(m)
	for len(m) < 4 {
		m = "0" + m
	}
	return m
}

// captureStatManifest lists every regular file and directory under dir inside the
// given pod, keyed by path relative to dir. find+stat reads the real on-disk mode
// on both source and target, so a special bit the filesystem silently dropped at
// chmod time isn't blamed on the transfer.
func captureStatManifest(k KubectlRunner, namespace, pod, dir string) (map[string]statEntry, error) {
	script := fmt.Sprintf(
		`cd %q && find . -mindepth 1 \( -type f -o -type d \) | sort | `+
			`while IFS= read -r p; do printf '%%s|%%s|%%s|%%s\n' "$p" "$(stat -c '%%a' "$p")" "$(stat -c '%%u' "$p")" "$(stat -c '%%g' "$p")"; done`,
		dir)
	out, err := k.Run("exec", pod, "-n", namespace, "--", "/bin/sh", "-c", script)
	if err != nil {
		return nil, fmt.Errorf("capture stat manifest under %q in pod %q (namespace %q): %w", dir, pod, namespace, err)
	}
	manifest := map[string]statEntry{}
	for _, line := range strings.Split(StripKubectlWarnings(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			return nil, fmt.Errorf("unexpected stat line %q under %q in pod %q", line, dir, pod)
		}
		manifest[parts[0]] = statEntry{mode: padMode(parts[1]), uid: parts[2], gid: parts[3]}
	}
	return manifest, nil
}

var _ = Describe("File metadata fidelity across an indirect transfer", func() {
	It("[MTA-923] Should preserve file permission bits and uniformly normalize UID/GID when migrating a PVC via S3",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			appName := "mysql"
			namespace := "indirect-metadata-fidelity"

			scenario := NewMigrationScenario(
				appName,
				namespace,
				config.K8sDeployBin,
				config.CraneBin,
				config.SourceContext,
				config.TargetContext,
			)
			srcApp := scenario.SrcAppNonAdmin
			tgtApp := scenario.TgtAppNonAdmin

			isOCP := scenario.KubectlSrc.IsOpenShift()
			srcApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
				"has_scc":        isOCP,
			}
			tgtApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
			}

			By("Grant namespace-admin permissions to the non-admin user on source and target")
			kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Delete test namespace on source and target (wait for completion)")
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
						log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
					}
				}
			})
			DeferCleanup(rbacCleanup)

			paths, err := NewScenarioPaths("crane-indirect-metadata-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			By("Deploy the source MySQL app (provides the <app>-data1 PVC mounted at /test-data)")
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			srcPod, err := GetPodNameByLabel(kubectlSrc, srcApp.Namespace, "app="+appName)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcPod).NotTo(BeEmpty(), "expected a running MySQL pod on source")
			Eventually(func() error {
				return waitForMySQLSocket(kubectlSrc, srcApp.Namespace, srcPod)
			}, "2m", "5s").Should(Succeed())

			By("Seed a matrix of files and directories with distinct permission bits on the source PVC")
			// Plain modes, special bits (setuid/setgid/sticky), nested directories, an
			// empty-directory set and a symlink. The manifest captured afterwards records
			// whatever the kernel actually set, and that captured value is the baseline.
			// The full matrix is seeded (not just the plain files asserted below) so each
			// run logs direct evidence of the PENDING gaps as well.
			seedScript := fmt.Sprintf(`set -e
D=%q
rm -rf "$D"
mkdir -p "$D"
for m in 600 640 644 664 700 750 755 444 400; do
  printf 'content-%%s\n' "$m" > "$D/file_$m"
  chmod "$m" "$D/file_$m"
done
printf 'setuid\n'  > "$D/file_setuid"; chmod 4755 "$D/file_setuid"
printf 'setgid\n'  > "$D/file_setgid"; chmod 2755 "$D/file_setgid"
mkdir -p "$D/dir_0750";   chmod 750  "$D/dir_0750"
mkdir -p "$D/dir_sticky"; chmod 1777 "$D/dir_sticky"
mkdir -p "$D/dir_setgid"; chmod 2775 "$D/dir_setgid"
mkdir -p "$D/nested/deep"; chmod 711 "$D/nested"; chmod 705 "$D/nested/deep"
printf 'nested-content\n' > "$D/nested/deep/file_640"; chmod 640 "$D/nested/deep/file_640"
ln -sf file_644 "$D/link_to_644"
sync`, permFidelityDir)
			_, err = kubectlSrc.Run("exec", srcPod, "-n", srcApp.Namespace, "--", "/bin/sh", "-c", seedScript)
			Expect(err).NotTo(HaveOccurred(), "failed to seed the permission matrix into the source /test-data PVC")

			By("Capture the source permission/ownership manifest")
			srcManifest, err := captureStatManifest(kubectlSrc, srcApp.Namespace, srcPod, permFidelityDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcManifest).NotTo(BeEmpty(), "expected to capture a non-empty source permission manifest")
			srcNames := make([]string, 0, len(srcManifest))
			for n := range srcManifest {
				srcNames = append(srcNames, n)
			}
			sort.Strings(srcNames)
			for _, n := range srcNames {
				e := srcManifest[n]
				log.Printf("SOURCE %-28s mode=%s uid=%s gid=%s", n, e.mode, e.uid, e.gid)
			}

			By("Quiesce the source app so the PVC copy is consistent and the volumes are released")
			Expect(kubectlSrc.ScaleDeploymentIfPresent(srcApp.Namespace, appName, 0)).NotTo(HaveOccurred())
			Eventually(func() (string, error) {
				out, err := kubectlSrc.Run("get", "pods", "-n", namespace, "-l", "app="+appName, "-o", "name")
				return strings.TrimSpace(StripKubectlWarnings(out)), err
			}, "90s", "3s").Should(BeEmpty())

			By("List the PVCs created by the source app")
			pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in namespace %q", srcApp.Namespace)
			log.Printf("Found %d PVCs in namespace %q", len(pvcs), srcApp.Namespace)
			targetStorageClass, err := DefaultStorageClassName(scenario.KubectlTgt.Context)
			Expect(err).NotTo(HaveOccurred())

			By("Transfer each PVC in indirect mode via S3")
			runner := scenario.CraneNonAdmin
			runner.WorkDir = paths.TempDir
			for _, pvc := range pvcs {
				opts := TransferPVCOptions{
					SourceContext:    srcApp.Context,
					TargetContext:    tgtApp.Context,
					PVCName:          pvc.Name,
					PVCNamespaceMap:  fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
					DestStorageClass: targetStorageClass,
					CloudStorage:     config.CloudStorage,
					RcloneConfigFile: config.RcloneConfigFile,
				}
				log.Printf("Transferring PVC %s via indirect S3", pvc.Name)
				Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred(),
					"indirect transfer-pvc should succeed for PVC %q", pvc.Name)
			}

			By("Wait for the ephemeral indirect-transfer pods to be removed")
			Eventually(func() (string, error) {
				return kubectlSrc.Run("get", "pods", "-n", srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return kubectlTgt.Run("get", "pods", "-n", tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))

			By("Verify all destination PVCs exist and locate the migrated /test-data PVC")
			tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())
			var dataPVC string
			for _, pvc := range tgtPVCs {
				Expect(VerifyPVCSchedulable(scenario.KubectlTgt.Context, tgtApp.Namespace, pvc.Name)).NotTo(HaveOccurred())
				if strings.Contains(pvc.Name, "data1") {
					dataPVC = pvc.Name
				}
			}
			Expect(dataPVC).NotTo(BeEmpty(), "expected to find the migrated /test-data PVC (<app>-data1) on target")

			By("Mount the migrated /test-data PVC in a verifier pod")
			const verifierPod = "mta-923-verifier"
			DeferCleanup(func() {
				if err := DeleteVerifierPod(kubectlTgt, tgtApp.Namespace, verifierPod); err != nil {
					log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
				}
			})
			Expect(DeployVerifierPod(kubectlTgt, VerifierPodOptions{
				Name:       verifierPod,
				Namespace:  tgtApp.Namespace,
				Image:      "quay.io/openshifttest/alpine:multiarch",
				Command:    []string{"sleep", "600"},
				Volumes:    []PodVolumeMount{{PVCName: dataPVC, MountPath: "/test-data"}},
				Restricted: true,
			})).NotTo(HaveOccurred())

			By("Capture the target permission/ownership manifest")
			tgtManifest, err := captureStatManifest(kubectlTgt, tgtApp.Namespace, verifierPod, permFidelityDir)
			Expect(err).NotTo(HaveOccurred())
			tgtNames := make([]string, 0, len(tgtManifest))
			for n := range tgtManifest {
				tgtNames = append(tgtNames, n)
			}
			sort.Strings(tgtNames)
			for _, n := range tgtNames {
				e := tgtManifest[n]
				log.Printf("TARGET %-28s mode=%s uid=%s gid=%s", n, e.mode, e.uid, e.gid)
			}

			// Log the full source->target delta as standing evidence of both the
			// preserved attributes and the documented PENDING gaps (missing empty dirs,
			// stripped special/dir mode bits, directory gid drift).
			By("Log the source->target metadata delta (evidence; gaps are tracked as PENDING specs)")
			for _, n := range srcNames {
				src := srcManifest[n]
				if tgt, ok := tgtManifest[n]; ok {
					log.Printf("DELTA  %-28s mode %s->%s  uid %s->%s  gid %s->%s", n, src.mode, tgt.mode, src.uid, tgt.uid, src.gid, tgt.gid)
				} else {
					log.Printf("DELTA  %-28s DROPPED (present on source mode=%s, absent on target)", n, src.mode)
				}
			}

			By("Verify regular-file permission bits are preserved across the indirect transfer")
			// The documented, currently-honored guarantee: ordinary regular files arrive
			// with identical mode bits.
			for _, n := range plainRegularFiles {
				src, ok := srcManifest[n]
				Expect(ok).To(BeTrue(), "test setup: expected to have seeded %q on the source", n)
				tgt, ok := tgtManifest[n]
				Expect(ok).To(BeTrue(), "regular file %q present on source is missing on target after transfer", n)
				Expect(tgt.mode).To(Equal(src.mode),
					"permission bits for regular file %q should survive the indirect transfer (source=%s target=%s)", n, src.mode, tgt.mode)
			}
			log.Printf("Permission bits preserved for all %d plain regular files", len(plainRegularFiles))

			By("Verify UID is uniformly normalized to a single value across every migrated entry")
			// Indirect transfer rewrites UID to the download pod's identity (crane-lib
			// --metadata-set uid), so source ownership does not carry across; the
			// guarantee verified here is uniformity — one consistent UID, no stray owner.
			Expect(tgtManifest).NotTo(BeEmpty())
			var normUID string
			for _, n := range tgtNames {
				e := tgtManifest[n]
				if normUID == "" {
					normUID = e.uid
				}
				Expect(e.uid).To(Equal(normUID),
					"all target entries should share one normalized uid; %q has uid=%s, expected %s", n, e.uid, normUID)
			}
			log.Printf("UID uniformly normalized on target to uid=%s (source entries were owned by uid=%s)",
				normUID, srcManifest[srcNames[0]].uid)

			By("Verify GID is uniformly normalized across regular files")
			// Indirect mode normalizes every downloaded file's group to one
			// resolved GID (RunAsGroup -> FSGroup -> UID). Verify that uniformity:
			// all regular files share one GID, no stray source group.
			var normFileGID string
			for _, n := range plainRegularFiles {
				tgt, ok := tgtManifest[n]
				Expect(ok).To(BeTrue(), "regular file %q present on source is missing on target after transfer", n)
				if normFileGID == "" {
					normFileGID = tgt.gid
				}
				Expect(tgt.gid).To(Equal(normFileGID),
					"all regular files should share one normalized gid; %q has gid=%s, expected %s", n, tgt.gid, normFileGID)
			}
			log.Printf("GID uniformly normalized across %d regular files to gid=%s (source regular files were owned by gid=%s)",
				len(plainRegularFiles), normFileGID, srcManifest[plainRegularFiles[0]].gid)
		})
})
