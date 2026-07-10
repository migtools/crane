package main

import (
	"os"
	"path/filepath"

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
		Use: filepath.Base(os.Args[0]),
	}
	f.ApplyFlags(&root)
	root.AddCommand(export.NewExportCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, f))
	root.AddCommand(transform.NewTransformCommand(f))
	root.AddCommand(apply.NewApplyCommand(f))
	root.AddCommand(version.NewVersionCommand(f))
	root.AddCommand(validate.NewValidateCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, f))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
