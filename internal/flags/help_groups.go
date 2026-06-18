package flags

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// KubernetesClientInheritedFlagNames returns the standard kube/client config
// flags added by genericclioptions.ConfigFlags.
func KubernetesClientInheritedFlagNames() []string {
	return []string{
		"as",
		"as-group",
		"as-uid",
		"as-user-extra",
		"cache-dir",
		"certificate-authority",
		"client-certificate",
		"client-key",
		"cluster",
		"context",
		"disable-compression",
		"insecure-skip-tls-verify",
		"kubeconfig",
		"namespace",
		"request-timeout",
		"server",
		"tls-server-name",
		"token",
		"user",
	}
}

// SetGroupedHelp configures command help output to separate local flags into:
// 1) command-specific flags
// 2) inherited Kubernetes/client flags (provided via inheritedFlagNames)
// Root/global inherited flags remain in Cobra's "Global Flags" section.
func SetGroupedHelp(cmd *cobra.Command, inheritedFlagNames []string) {
	inherited := make(map[string]struct{}, len(inheritedFlagNames))
	for _, name := range inheritedFlagNames {
		inherited[name] = struct{}{}
	}

	cmd.SetUsageFunc(func(c *cobra.Command) error {
		out := c.OutOrStdout()
		if _, err := fmt.Fprintf(out, "Usage:\n  %s\n\n", c.UseLine()); err != nil {
			return err
		}
		commandSpecific := pflag.NewFlagSet("command-specific", pflag.ContinueOnError)
		kubeClient := pflag.NewFlagSet("kube-client", pflag.ContinueOnError)

		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if _, ok := inherited[f.Name]; ok {
				kubeClient.AddFlag(f)
				return
			}
			commandSpecific.AddFlag(f)
		})

		if commandSpecific.HasAvailableFlags() {
			if _, err := fmt.Fprintln(out, "Command-specific Flags:"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, commandSpecific.FlagUsages()); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}

		if kubeClient.HasAvailableFlags() {
			if _, err := fmt.Fprintln(out, "Inherited Kubernetes Client Flags:"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, kubeClient.FlagUsages()); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}

		if c.InheritedFlags().HasAvailableFlags() {
			if _, err := fmt.Fprintln(out, "Global Flags:"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, c.InheritedFlags().FlagUsages()); err != nil {
				return err
			}
		}
		return nil
	})
}
