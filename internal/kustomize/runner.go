package kustomize

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// Runner wraps the krusty API to build kustomizations without shelling out to kubectl/oc.
type Runner struct {
	Log  *logrus.Logger
	Args []string
}

// Build runs kustomize build on the given directory and returns the rendered YAML.
func (r *Runner) Build(dir string) ([]byte, error) {
	r.Log.Debugf("Running kustomize build on %s", dir)

	opts, envVars, err := r.buildOptions()
	if err != nil {
		r.Log.Debugf("Failed to build kustomize options: %v", err)
		return nil, fmt.Errorf("failed to build kustomize options: %w", err)
	}

	// Set environment variables (used by helm) and restore after build
	restoreEnv, err := setEnvVars(envVars)
	if err != nil {
		r.Log.Warnf("Failed to set environment variables for kustomize: %v", err)
		return nil, err
	}
	defer restoreEnv()

	k := krusty.MakeKustomizer(opts)
	resMap, err := k.Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		r.Log.Debugf("Kustomize build failed for %s: %v", dir, err)
		return nil, fmt.Errorf("kustomize build failed for %s: %w", dir, err)
	}

	yamlBytes, err := resMap.AsYaml()
	if err != nil {
		r.Log.Debugf("Failed to serialize kustomize output for %s: %v", dir, err)
		return nil, fmt.Errorf("failed to serialize kustomize output: %w", err)
	}

	r.Log.Debugf("Kustomize build completed for %s (%d bytes)", dir, len(yamlBytes))
	return yamlBytes, nil
}

// buildOptions maps CLI args to krusty.Options.
// LoadRestrictions and PluginRestrictions are hardcoded to permissive defaults
// since crane controls the kustomization filesystem and doesn't need CLI-era restrictions.
func (r *Runner) buildOptions() (*krusty.Options, []envVar, error) {
	opts := krusty.MakeDefaultOptions()
	opts.Reorder = krusty.ReorderOptionLegacy
	opts.LoadRestrictions = types.LoadRestrictionsNone
	opts.PluginConfig.PluginRestrictions = types.PluginRestrictionsNone
	var envVars []envVar

	for i := 0; i < len(r.Args); i++ {
		arg := r.Args[i]

		switch {
		case arg == "--enable-helm":
			opts.PluginConfig.HelmConfig.Enabled = true

		case arg == "--helm-command":
			if i+1 >= len(r.Args) {
				r.Log.Debugf("--helm-command requires a value")
				return nil, nil, fmt.Errorf("--helm-command requires a value")
			}
			i++
			opts.PluginConfig.HelmConfig.Enabled = true
			opts.PluginConfig.HelmConfig.Command = r.Args[i]

		case strings.HasPrefix(arg, "--helm-command="):
			val := strings.SplitN(arg, "=", 2)[1]
			opts.PluginConfig.HelmConfig.Enabled = true
			opts.PluginConfig.HelmConfig.Command = val

		case arg == "--env" || arg == "-e":
			if i+1 >= len(r.Args) {
				r.Log.Debugf("%s requires a value", arg)
				return nil, nil, fmt.Errorf("%s requires a value", arg)
			}
			i++
			parts := strings.SplitN(r.Args[i], "=", 2)
			if len(parts) != 2 {
				r.Log.Debugf("Invalid env format %q, expected KEY=VALUE", r.Args[i])
				return nil, nil, fmt.Errorf("invalid env format %q, expected KEY=VALUE", r.Args[i])
			}
			envVars = append(envVars, envVar{key: parts[0], value: parts[1]})

		default:
			r.Log.Debugf("Unsupported kustomize argument: %q", arg)
			return nil, nil, fmt.Errorf("unsupported kustomize argument: %q", arg)
		}
	}

	return opts, envVars, nil
}

type envVar struct {
	key, value string
}

func setEnvVars(vars []envVar) (func(), error) {
	originals := make(map[string]string)
	unsetKeys := make([]string, 0)

	for _, v := range vars {
		if orig, exists := os.LookupEnv(v.key); exists {
			originals[v.key] = orig
		} else {
			unsetKeys = append(unsetKeys, v.key)
		}
		if err := os.Setenv(v.key, v.value); err != nil {
			return nil, fmt.Errorf("failed to set env %q: %w", v.key, err)
		}
	}

	return func() {
		for k, v := range originals {
			_ = os.Setenv(k, v)
		}
		for _, k := range unsetKeys {
			_ = os.Unsetenv(k)
		}
	}, nil
}
