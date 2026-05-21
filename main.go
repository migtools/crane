package main

import (
	"os"

	"github.com/konveyor/crane/cmd/apply"
	export "github.com/konveyor/crane/cmd/export"
	"github.com/konveyor/crane/cmd/transform"
	"github.com/konveyor/crane/cmd/validate"
	"github.com/konveyor/crane/cmd/version"
	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	f := &flags.GlobalFlags{}
	root := cobra.Command{
		Use: "mta-ops",
	}
	f.ApplyFlags(&root)
	root.AddCommand(export.NewExportCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, f))
	root.AddCommand(transform.NewTransformCommand(f))
	root.AddCommand(apply.NewApplyCommand(f))
	root.AddCommand(validate.NewValidateCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, f))

	// Disabled commands for this branch/release:
	// root.AddCommand(transfer_pvc.NewTransferPVCCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	// root.AddCommand(tunnel_api.NewTunnelAPIOptions(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	// root.AddCommand(convert.NewConvertOptions(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	// root.AddCommand(skopeo_sync_gen.NewSkopeoSyncGenCommand(f))
	// root.AddCommand(plugin_manager.NewPluginManagerCommand(f))
	root.AddCommand(version.NewVersionCommand(f))

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
