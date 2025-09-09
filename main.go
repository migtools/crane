package main

import (
	"os"

	"github.com/konveyor/crane/cmd/apply"
	"github.com/konveyor/crane/cmd/convert"
	export "github.com/konveyor/crane/cmd/export"
	plugin_manager "github.com/konveyor/crane/cmd/plugin-manager"
	"github.com/konveyor/crane/cmd/runfn"
	skopeo_sync_gen "github.com/konveyor/crane/cmd/skopeo-sync-gen"
	transfer_pvc "github.com/konveyor/crane/cmd/transfer-pvc"
	"github.com/konveyor/crane/cmd/transform"
	tunnel_api "github.com/konveyor/crane/cmd/tunnel-api"
	"github.com/konveyor/crane/cmd/version"
	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	f := &flags.GlobalFlags{}
	root := cobra.Command{
		Use: "crane",
	}
	f.ApplyFlags(&root)
	root.AddCommand(export.NewExportCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, f))
	root.AddCommand(transfer_pvc.NewTransferPVCCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	root.AddCommand(tunnel_api.NewTunnelAPIOptions(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	root.AddCommand(convert.NewConvertOptions(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	root.AddCommand(transform.NewTransformCommand(f))
	root.AddCommand(skopeo_sync_gen.NewSkopeoSyncGenCommand(f))
	root.AddCommand(apply.NewApplyCommand(f))
	root.AddCommand(plugin_manager.NewPluginManagerCommand(f))
	root.AddCommand(version.NewVersionCommand(f))
	root.AddCommand(runfn.NewFnRunCommand(f))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
