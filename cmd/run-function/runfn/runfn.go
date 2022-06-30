// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

// Package runfn
//Reference: sigs.k8s.io/kustomize/kyaml/runfn
package runfn

import (
	"io"
	"sigs.k8s.io/kustomize/kyaml/errors"
	"sigs.k8s.io/kustomize/kyaml/kio"

	"path/filepath"
	"sigs.k8s.io/kustomize/kyaml/fn/runtime/container"
	"sigs.k8s.io/kustomize/kyaml/fn/runtime/runtimeutil"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// RunFns runs the set of configuration functions in a local directory against
// the Resources in that directory
type RunFns struct {
	// Path is the path to the directory containing resources to run the functions on
	Path string

	// Function is a function to run against the input.
	Function *runtimeutil.FunctionSpec

	// FnConfig is the configurations passed from command line
	FnConfig *yaml.RNode

	// Output can be set to write the result to Output
	Output io.Writer

	// Env contains environment variables that will be exported to container
	Env []string

	// functionFilterProvider provides a filter to perform the function.
	// this is a variable, so it can be mocked in tests
	functionFilterProvider func(filter runtimeutil.FunctionSpec, fnConfig *yaml.RNode) (kio.Filter, error)
}

// Execute runs the command
func (r RunFns) Execute() error {
	// make the path absolute so it works on Mac
	var err error
	r.Path, err = filepath.Abs(r.Path)
	if err != nil {
		return errors.Wrap(err)
	}

	// default the containerFilterProvider if it hasn't been override.
	err = (&r).init()
	if err != nil {
		return err
	}
	nodes, filters, err := r.getNodesAndFilters()
	if err != nil {
		return err
	}
	return r.runFunctions(nodes, filters)
}

func (r RunFns) getNodesAndFilters() (*kio.LocalPackageReader, []kio.Filter, error) {
	// Read Resources from Directory
	inputResources := &kio.LocalPackageReader{
		PackagePath:       r.Path,
		MatchFilesGlob:    kio.MatchAll,
		PreserveSeqIndent: true,
		WrapBareSeqNode:   true,
	}

	filters, err := r.getFilters()
	if err != nil {
		return nil, nil, err
	}
	return inputResources, filters, nil
}

func (r RunFns) getFilters() ([]kio.Filter, error) {
	spec := r.Function
	if spec == nil {
		return nil, nil
	}

	containerFilter, err := r.functionFilterProvider(*spec, r.FnConfig)
	if err != nil {
		return nil, err
	}

	if containerFilter == nil {
		return nil, nil
	}
	return []kio.Filter{containerFilter}, nil
}

// runFunctions runs the filters against the input and writes to r.Output
func (r RunFns) runFunctions(input kio.Reader, filters []kio.Filter) error {
	output := kio.ByteWriter{
		Writer:                r.Output,
		KeepReaderAnnotations: true,
		WrappingKind:          kio.ResourceListKind,
		WrappingAPIVersion:    kio.ResourceListAPIVersion,
	}

	pipeline := kio.Pipeline{
		Inputs:                []kio.Reader{input},
		Filters:               filters,
		Outputs:               []kio.Writer{output},
		ContinueOnEmptyResult: true,
	}

	err := pipeline.Execute()
	return err
}

// init initializes the RunFns with a containerFilterProvider.
func (r *RunFns) init() error {
	if r.functionFilterProvider == nil {
		r.functionFilterProvider = r.defaultFnFilterProvider
	}
	return nil
}

// defaultFnFilterProvider provides function filters
func (r *RunFns) defaultFnFilterProvider(spec runtimeutil.FunctionSpec, fnConfig *yaml.RNode) (kio.Filter, error) {
	c := container.NewContainer(
		runtimeutil.ContainerSpec{
			Image: spec.Container.Image,
			Env:   spec.Container.Env,
		},
		"nobody",
	)
	cf := &c
	cf.Exec.FunctionConfig = fnConfig
	cf.Exec.GlobalScope = false
	cf.Exec.DeferFailure = spec.DeferFailure
	return cf, nil
}
