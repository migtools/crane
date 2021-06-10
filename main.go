package main

import (
	"os"

	export "github.com/konveyor/crane/cmd/export"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	root := cobra.Command{
		Use: "crane",
	}
	root.AddCommand(export.NewExportCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
