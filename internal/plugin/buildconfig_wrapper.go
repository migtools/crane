package plugin

import (
	"github.com/konveyor/crane-lib/transform"
	"github.com/migtools/crane-plugin-buildconfig-to-shipwright/buildconfig"
	"github.com/sirupsen/logrus"
)

// BuildConfigToBuildsPluginWrapper wraps the buildconfig plugin to override its name
type BuildConfigToBuildsPluginWrapper struct {
	*buildconfig.BuildConfigTransformPlugin
}

// NewBuildConfigToBuildsPlugin creates a new instance with custom name
func NewBuildConfigToBuildsPlugin(logger *logrus.Logger) *BuildConfigToBuildsPluginWrapper {
	return &BuildConfigToBuildsPluginWrapper{
		BuildConfigTransformPlugin: &buildconfig.BuildConfigTransformPlugin{
			Log: logger,
		},
	}
}

// Metadata overrides the original plugin's metadata to use custom name
func (p *BuildConfigToBuildsPluginWrapper) Metadata() transform.PluginMetadata {
	// Get original metadata
	original := p.BuildConfigTransformPlugin.Metadata()

	// Override only the name
	original.Name = "BuildConfigToBuildsPlugin"

	return original
}
