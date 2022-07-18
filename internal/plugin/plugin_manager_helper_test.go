package plugin

import (
	"io/ioutil"
	"net/http"
	"os"
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
		Kind: "PluginIndex",
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
		Kind: "PluginIndex",
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

func TestYamlToManifestWithFile(t *testing.T) {
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
