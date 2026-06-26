// TestYamlToManifest* use a manifest with only a linux/amd64 binary.
package plugin

import (
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/jarcoal/httpmock"
	"gotest.tools/v3/assert"
)

func TestGetYamlFromUrlWithUrl(t *testing.T) {
	URL := "https://test.com/index.yml"
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	index := PluginIndex{
		Kind:       "PluginIndex",
		ApiVersion: "crane.konveyor.io/v1alpha1",
		Plugins: []PluginLocation{
			{
				Name: "foo",
				Path: "https://test.com/foo.yml",
			},
		},
	}
	httpmock.RegisterResponder("GET", URL,
		func(req *http.Request) (*http.Response, error) {
			// Get ID from request
			return httpmock.NewJsonResponse(200, index)
		},
	)

	resp, _ := GetYamlFromUrl(URL)
	assert.DeepEqual(t, index, resp)
}

func TestYamlToManifestWithUrl(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("original fixture is linux/amd64-only; assertions run on linux/amd64")
	}
	URL := "https://test.com/foo.yml"
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	plugin := Plugin{
		Kind:       "Plugin",
		ApiVersion: "crane.konveyor.io/v1alpha1",
		Versions: []PluginVersion{
			{
				Name:             "foo",
				Version:          "0.0.1",
				Description:      "Description of foo plugin",
				ShortDescription: "Short description of foo plugin",
				Binaries: []Binary{
					{
						OS:   "linux",
						Arch: "amd64",
						URI:  "https://test.com/download/foo",
					},
				},
			},
		},
	}
	httpmock.RegisterResponder("GET", URL,
		func(req *http.Request) (*http.Response, error) {
			// Get ID from request
			return httpmock.NewJsonResponse(200, plugin)
		},
	)

	resp, _ := YamlToManifest(URL)
	assert.DeepEqual(t, plugin.Versions, resp)
}

func TestGetYamlFromUrlWithFile(t *testing.T) {
	index := PluginIndex{
		Kind:       "PluginIndex",
		ApiVersion: "crane.konveyor.io/v1alpha1",
		Plugins: []PluginLocation{
			{
				Name: "foo",
				Path: "https://test.com/foo.yml",
			},
		},
	}
	tempFile, err := ioutil.TempFile(os.TempDir(), "index.yml")
	if err != nil {
		panic(err)
	}
	defer os.Remove(tempFile.Name())

	data, err := yaml.Marshal(&index)
	if err != nil {
		panic(err)
	}

	_, err = tempFile.Write(data)
	if err != nil {
		panic(err)
	}

	resp, _ := GetYamlFromUrl(tempFile.Name())
	assert.DeepEqual(t, index, resp)

	resp, _ = GetYamlFromUrl("file://" + tempFile.Name())
	assert.DeepEqual(t, index, resp)
}

func TestFilterPluginForOsArch_PersistsFilter(t *testing.T) {
	p := Plugin{
		Versions: []PluginVersion{
			{
				Name:    "TestPlugin",
				Version: "0.0.1",
				Binaries: []Binary{
					{OS: "linux", Arch: "amd64", URI: "https://example.com/linux-amd64"},
					{OS: "darwin", Arch: "amd64", URI: "https://example.com/darwin-amd64"},
					{OS: "darwin", Arch: "arm64", URI: "https://example.com/darwin-arm64"},
				},
			},
		},
	}

	available := FilterPluginForOsArch(&p)

	if !available {
		t.Fatal("expected plugin to be available for current platform")
	}

	// After filtering, each version should have exactly 1 binary matching current OS/arch
	for _, version := range p.Versions {
		if len(version.Binaries) != 1 {
			t.Fatalf("expected 1 binary after filter, got %d", len(version.Binaries))
		}
		if version.Binaries[0].OS != runtime.GOOS {
			t.Errorf("binary OS = %q, want %q", version.Binaries[0].OS, runtime.GOOS)
		}
		if version.Binaries[0].Arch != runtime.GOARCH {
			t.Errorf("binary Arch = %q, want %q", version.Binaries[0].Arch, runtime.GOARCH)
		}
	}
}

func TestFilterPluginForOsArch_NoPlatformMatch(t *testing.T) {
	p := Plugin{
		Versions: []PluginVersion{
			{
				Name:    "TestPlugin",
				Version: "0.0.1",
				Binaries: []Binary{
					{OS: "plan9", Arch: "mips", URI: "https://example.com/plan9-mips"},
				},
			},
		},
	}

	available := FilterPluginForOsArch(&p)

	if available {
		t.Fatal("expected plugin to be unavailable for non-matching platform")
	}
}

func TestYamlToManifestWithFile(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("original fixture is linux/amd64-only; assertions run on linux/amd64")
	}
	plugin := Plugin{
		Kind:       "Plugin",
		ApiVersion: "crane.konveyor.io/v1alpha1",
		Versions: []PluginVersion{
			{
				Name:             "foo",
				Version:          "0.0.1",
				Description:      "Description of foo plugin",
				ShortDescription: "Short description of foo plugin",
				Binaries: []Binary{
					{
						OS:   "linux",
						Arch: "amd64",
						URI:  "https://test.com/download/foo",
					},
				},
			},
		},
	}
	tempFile, err := ioutil.TempFile(os.TempDir(), "index.yml")
	if err != nil {
		panic(err)
	}
	defer os.Remove(tempFile.Name())

	data, err := yaml.Marshal(&plugin)
	if err != nil {
		panic(err)
	}

	_, err = tempFile.Write(data)
	if err != nil {
		panic(err)
	}

	resp, _ := YamlToManifest(tempFile.Name())
	assert.DeepEqual(t, plugin.Versions, resp)

	resp, _ = YamlToManifest("file://" + tempFile.Name())
	assert.DeepEqual(t, plugin.Versions, resp)
}
