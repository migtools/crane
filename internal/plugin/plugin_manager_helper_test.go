package plugin

import (
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/jarcoal/httpmock"
	"gotest.tools/assert"
)

func TestGetYamlFromUrlWithUrl(t *testing.T) {
	URL := "https://test.com/index.yml"
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	index := map[string]interface{}{
		"foo-0.0.1": "https://test.com/foo-0.0.1.yml",
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
	URL := "https://test.com/foo-0.0.1.yml"
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	index := Manifest{
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
	}
	httpmock.RegisterResponder("GET", URL,
		func(req *http.Request) (*http.Response, error) {
			// Get ID from request
			return httpmock.NewJsonResponse(200, index)
		},
	)

	resp, _ := YamlToManifest(URL)
	assert.DeepEqual(t, index, resp)
}

func TestGetYamlFromUrlWithFile(t *testing.T) {
	index := map[string]interface{}{
		"foo-0.0.1": "file://tmp/foo-0.0.1.yml",
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
	plugin := Manifest{
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
	assert.DeepEqual(t, plugin, resp)

	resp, _ = YamlToManifest("file://" + tempFile.Name())
	assert.DeepEqual(t, plugin, resp)
}
