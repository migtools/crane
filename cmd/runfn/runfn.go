package runfn

import (
	"bytes"
	"fmt"
	"github.com/konveyor/crane/internal/flags"
	"io"
	"sigs.k8s.io/kustomize/kyaml/yaml"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/kustomize/cmd/config/runner"
	"sigs.k8s.io/kustomize/kyaml/errors"
	"sigs.k8s.io/kustomize/kyaml/fn/runtime/runtimeutil"
	"sigs.k8s.io/kustomize/kyaml/runfn"
)

type Options struct {
	// cobraGlobalFlags for explicit CLI args parsed by cobra
	cobraGlobalFlags *flags.GlobalFlags
	Flags
}

type Flags struct {
	ExportDir          string
	TransformDir       string
	Image              string
	Env                []string
	RunFns             runfn.RunFns
	TransformedContent bytes.Buffer
}

func NewFnRunCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:          "runfn [IMAGE] [flags] [--args]",
		Short:        "Transform resources by executing function",
		RunE:         o.runE,
		PreRunE:      o.preRunE,
		SilenceUsage: true,
	}
	addFlagsForOptions(&o.Flags, cmd)
	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export",
		fmt.Sprintf("Path to the local directory containing exported resources"))
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform",
		fmt.Sprintf("The path where transformed resources are written"))
	cmd.Flags().StringArrayVarP(
		&o.Env, "env", "", []string{},
		"a list of environment variables to be used by functions")
}

func (o *Options) runE(c *cobra.Command, _ []string) error {
	if err := runner.HandleError(c, o.RunFns.Execute()); err != nil {
		return err
	}
	if err := WriteOutput(o.TransformDir, o.TransformedContent.String()); err != nil {
		return err
	}
	fmt.Println("Transformed resources are written to:", o.TransformDir)
	return nil
}

func (o *Options) preRunE(c *cobra.Command, args []string) error {
	//check if export dir exists
	if !checkIfDirExists(o.ExportDir) {
		return fmt.Errorf("export-dir %s does not exist", o.ExportDir)
	}

	//check if transform dir does not already exist
	if checkIfDirExists(o.TransformDir) {
		return fmt.Errorf("transform-dir %s already exist", o.TransformDir)
	}

	var fnArgs []string
	if c.ArgsLenAtDash() >= 0 {
		fnArgs = append(fnArgs, args[c.ArgsLenAtDash():]...)
		args = args[:c.ArgsLenAtDash()]
	}

	var err error
	if o.Image, err = getFunctionImage(args); err != nil {
		return err
	}

	fns, err := o.getContainerFunctions(fnArgs)
	if err != nil {
		return err
	}

	// set the output
	var output io.Writer
	o.TransformedContent = bytes.Buffer{}
	output = &o.TransformedContent

	o.RunFns = runfn.RunFns{
		Path:      o.ExportDir,
		Output:    output,
		Functions: fns,
		Env:       o.Env,
	}
	return nil
}

// getFunctionImage parses the commandline arguments and return image string
func getFunctionImage(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.Errorf("must specify image to run a function")
	} else if len(args) == 1 {
		return args[0], nil
	} else {
		return "", errors.Errorf("1 argument supported, function arguments go after '--'")
	}
}

// getContainerFunctions parses the commandline flags and arguments into explicit
// Functions to run.
func (o *Options) getContainerFunctions(dataItems []string) ([]*yaml.RNode, error) {
	res, err := getFunctionConfig(dataItems)
	if err != nil {
		return nil, err
	}

	// create the function spec to set as an annotation
	fnAnnotation, err := o.getFunctionAnnotation()

	if err != nil {
		return nil, err
	}

	// set the function annotation on the function config, so that it is parsed by RunFns
	value, err := fnAnnotation.String()
	if err != nil {
		return nil, errors.Wrap(err)
	}
	if err = res.PipeE(
		yaml.LookupCreate(yaml.MappingNode, "metadata", "annotations"),
		yaml.SetField(runtimeutil.FunctionAnnotationKey, yaml.NewScalarRNode(value))); err != nil {
		return nil, errors.Wrap(err)
	}

	return []*yaml.RNode{res}, nil
}

func (o *Options) getFunctionAnnotation() (*yaml.RNode, error) {
	if err := ValidateFunctionImageURL(o.Image); err != nil {
		return nil, err
	}

	fn, err := yaml.Parse(`container: {}`)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	if err = fn.PipeE(
		yaml.Lookup("container"),
		yaml.SetField("image", yaml.NewScalarRNode(o.Image))); err != nil {
		return nil, errors.Wrap(err)
	}
	return fn, nil
}

// getFunctionConfig parses the commandline flags and arguments into explicit function config
func getFunctionConfig(fnArgs []string) (*yaml.RNode, error) {
	// create the function config
	rc, err := yaml.Parse(`
metadata:
  name: function-input
data: {}
`)
	if err != nil {
		return nil, err
	}

	// default the function config kind to ConfigMap, this may be overridden
	var kind = "ConfigMap"
	var version = "v1"

	// populate the function config with data.  this is a convention for functions
	// to be more commandline friendly
	if len(fnArgs) > 0 {
		dataField, err := rc.Pipe(yaml.Lookup("data"))
		if err != nil {
			return nil, err
		}
		for i, s := range fnArgs {
			kv := strings.SplitN(s, "=", 2)
			if i == 0 && len(kv) == 1 {
				// first argument may be the kind
				kind = s
				continue
			}
			if len(kv) != 2 {
				return nil, fmt.Errorf("args must have keys and values separated by =")
			}
			// When we are using a ConfigMap as the functionConfig, we should create
			// the node with type string instead of creating a scalar node. Because
			// a scalar node might be parsed as int, float or bool later.
			err := dataField.PipeE(yaml.SetField(kv[0], yaml.NewStringRNode(kv[1])))
			if err != nil {
				return nil, err
			}
		}
	}
	if err = rc.PipeE(yaml.SetField("kind", yaml.NewScalarRNode(kind))); err != nil {
		return nil, err
	}
	if err = rc.PipeE(yaml.SetField("apiVersion", yaml.NewScalarRNode(version))); err != nil {
		return nil, err
	}
	return rc, nil
}
