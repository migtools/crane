package main

import (
	"os"

	"github.com/konveyor/crane/cmd/apply"
	export "github.com/konveyor/crane/cmd/export"
	"github.com/konveyor/crane/cmd/transform"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	root := cobra.Command{
		Use: "crane",
	}
	root.AddCommand(export.NewExportCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	root.AddCommand(transform.NewTransformCommand())
	root.AddCommand(apply.NewApplyCommand())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
