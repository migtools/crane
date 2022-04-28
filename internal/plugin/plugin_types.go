package plugin

import "github.com/konveyor/crane-lib/transform"

type Plugin struct {
	Kind       string   `json:"kind"`
	ApiVersion string   `json:"apiVersion"`
	Versions []PluginVersion `json:"versions"`
}

type PluginVersion struct {
	Name             string                     `json:"name"`
	ShortDescription string                     `json:"shortDescription"`
	Description      string                     `json:"description"`
	Version          Version                    `json:"version"`
	Binaries         []Binary                   `json:"binaries"`
	OptionalFields   []transform.OptionalFields `json:"optionalFields"`
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
	Kind       string   `json:"kind"`
	ApiVersion string   `json:"apiVersion"`
	Plugins    []PluginLocation `json:"plugins"`
}
