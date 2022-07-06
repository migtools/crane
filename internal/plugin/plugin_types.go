package plugin

import (
	"errors"
	"strings"

	"github.com/konveyor/crane-lib/transform"
	"golang.org/x/mod/semver"
)

// partial copy from apimachinery
// no need for direct dependency as these fields are stable.
type ObjectMeta struct {
	Name        string            `json:"name,omitempty" yaml:"name,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// partial copy from apimachinery
// no need for direct dependency as these fields are stable.
type TypeMeta struct {
	Kind       string `json:"kind,omitempty" yaml:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
}

type Plugin struct {
	TypeMeta `json:",inline" yaml:",inline"`
	MetaData *ObjectMeta     `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Versions []PluginVersion `json:"versions"`
}

type PluginVersion struct {
	Version        Version                    `json:"version"`
	Binaries       []Binary                   `json:"binaries"`
	OptionalFields []transform.OptionalFields `json:"optionalFields"`
}

type Version string

type Binary struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	URI  string `json:"uri"`
	SHA  string `json:"sha,omitempty"`
}

type PluginLocation struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type PluginIndex struct {
	Kind       string           `json:"kind"`
	ApiVersion string           `json:"apiVersion"`
	Plugins    []PluginLocation `json:"plugins"`
}

// Name returns the name of the plugin
func (plug *Plugin) Name() string {
	if plug.MetaData == nil {
		return ""
	}
	return plug.MetaData.Name
}

// Description returns the description of a plugin
func (plug *Plugin) Description() string {
	if plug.MetaData == nil || len(plug.MetaData.Annotations) == 0 {
		return ""
	}
	return plug.MetaData.Annotations["description"]
}

// VersionsString teturns the versions of a plugin as a string
func (plug *Plugin) VersionsString() string {
	versions := make([]string, len(plug.Versions))
	for i, version := range plug.Versions {
		versions[i] = string(version.Version)
	}
	return strings.Join(versions, ", ")
}

// LatestVersion returns the latest version for a particular plugin.
func (plug *Plugin) LatestVersion() string {
	numVersions := len(plug.Versions)
	versions := make([]string, numVersions)
	for i, version := range plug.Versions {
		versions[i] = string(version.Version)
	}
	semver.Sort(versions)
	return versions[numVersions - 1]
}

// GetVersion returns the specified PluginVersion specified. If version is
// empty string, this will return the latest version. If no version is found an
// error is returned.
func (plug *Plugin) GetVersion(version string) (*PluginVersion, error) {
	if version  == "" {
		version = plug.LatestVersion()
	}
	for i, plugVersion := range plug.Versions {
		if string(plugVersion.Version) == version {
			return &plug.Versions[i], nil
		}
	}

	return nil, errors.New("Version not found")
}
