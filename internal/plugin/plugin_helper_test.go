package plugin

import (
	"errors"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTransformPlugin struct {
	name    string
	version string
}

func (p fakeTransformPlugin) Metadata() transform.PluginMetadata {
	return transform.PluginMetadata{Name: p.name, Version: p.version}
}

func (p fakeTransformPlugin) Run(transform.PluginRequest) (transform.PluginResponse, error) {
	return transform.PluginResponse{}, nil
}

func TestDiscoverPluginDescriptors(t *testing.T) {
	paths := []string{"/project/plugins", "/explicit/plugins", GlobalPluginDir, PkgPluginDir}
	pluginsByPath := map[string][]transform.Plugin{
		paths[0]: {
			fakeTransformPlugin{name: "EmbeddedDefaultPlugin", version: "external"},
			fakeTransformPlugin{name: "LocalPlugin", version: "local"},
			fakeTransformPlugin{name: "DuplicatePlugin", version: "local"},
		},
		paths[1]: {
			fakeTransformPlugin{name: "ExplicitPlugin", version: "explicit"},
			fakeTransformPlugin{name: "DuplicatePlugin", version: "explicit"},
		},
		paths[2]: {
			fakeTransformPlugin{name: "GlobalPlugin", version: "global"},
		},
		paths[3]: {
			fakeTransformPlugin{name: "PackagePlugin", version: "package"},
		},
	}
	builtIns := []builtInPlugin{
		{plugin: fakeTransformPlugin{name: "EmbeddedDefaultPlugin", version: "embedded"}, enabledByDefault: true},
		{plugin: fakeTransformPlugin{name: "EmbeddedOptInPlugin", version: "embedded"}, enabledByDefault: false},
	}

	descriptors, err := discoverPluginDescriptors(builtIns, paths, nil, func(path string) ([]transform.Plugin, error) {
		return pluginsByPath[path], nil
	})
	require.NoError(t, err)
	require.Len(t, descriptors, 7)

	assert.Equal(t, []string{
		"EmbeddedDefaultPlugin",
		"EmbeddedOptInPlugin",
		"LocalPlugin",
		"DuplicatePlugin",
		"ExplicitPlugin",
		"GlobalPlugin",
		"PackagePlugin",
	}, descriptorNames(descriptors))

	assert.Equal(t, PluginSourceEmbedded, descriptors[0].Source)
	assert.Empty(t, descriptors[0].SourceDirectory)
	assert.True(t, descriptors[0].EnabledByDefault)
	assert.Equal(t, "embedded", descriptors[0].Plugin.Metadata().Version)
	assert.False(t, descriptors[1].EnabledByDefault)

	wantExternalSources := map[string]string{
		"LocalPlugin":     paths[0],
		"DuplicatePlugin": paths[0],
		"ExplicitPlugin":  paths[1],
		"GlobalPlugin":    paths[2],
		"PackagePlugin":   paths[3],
	}
	for _, descriptor := range descriptors[2:] {
		assert.Equal(t, PluginSourceExternal, descriptor.Source)
		assert.Equal(t, wantExternalSources[descriptor.Plugin.Metadata().Name], descriptor.SourceDirectory)
		assert.True(t, descriptor.EnabledByDefault)
	}
	assert.Equal(t, "local", descriptors[3].Plugin.Metadata().Version)
}

func TestDiscoverPluginDescriptorsAppliesSkipPlugins(t *testing.T) {
	builtIns := []builtInPlugin{
		{plugin: fakeTransformPlugin{name: "EmbeddedPlugin"}, enabledByDefault: true},
	}

	descriptors, err := discoverPluginDescriptors(builtIns, []string{"/plugins"}, []string{"EmbeddedPlugin", "ExternalPlugin"}, func(string) ([]transform.Plugin, error) {
		return []transform.Plugin{fakeTransformPlugin{name: "ExternalPlugin"}}, nil
	})
	require.NoError(t, err)
	assert.Empty(t, descriptors)
}

func TestDiscoverPluginDescriptorsReturnsLoaderError(t *testing.T) {
	wantErr := errors.New("cannot read plugin directory")

	_, err := discoverPluginDescriptors(nil, []string{"/plugins"}, nil, func(string) ([]transform.Plugin, error) {
		return nil, wantErr
	})

	assert.ErrorIs(t, err, wantErr)
}

func descriptorNames(descriptors []PluginDescriptor) []string {
	names := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		names[i] = descriptor.Plugin.Metadata().Name
	}
	return names
}
