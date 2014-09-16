package main

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v1"
	"io/ioutil"
)

// RawBox is the data type for a box in the wercker.yml
type RawBox string

// RawBuild is the data type for builds in the wercker.yml
type RawBuild struct {
	RawSteps []interface{} `yaml:"steps"`
}

// RawConfig is the data type for wercker.yml
type RawConfig struct {
	SourceDir string    `yaml:"source-dir"`
	RawBox    *RawBox   `yaml:"box"`
	RawBuild  *RawBuild `yaml:"build"`
}

// RawStep is the data type for a step in wercker.yml
type RawStep map[string]RawStepData

// RawStepData is the data type for the contents of a step in wercker.yml
type RawStepData map[string]string

// ReadWerckerYaml will try to find a wercker.yml file and return its bytes.
// TODO(termie): If allowDefault is true it will try to generate a
// default yaml file by inspecting the project.
func ReadWerckerYaml(searchDirs []string, allowDefault bool) ([]byte, error) {
	var foundYaml string
	found := false

	for _, v := range searchDirs {
		possibleYaml := fmt.Sprintf("%s/wercker.yml", v)
		ymlExists, err := exists(possibleYaml)
		if err != nil {
			return nil, err
		}
		if !ymlExists {
			continue
		}
		found = true
		foundYaml = possibleYaml
	}

	// TODO(termie): If allowDefault, we'd generate something here
	if !allowDefault && !found {
		return nil, errors.New("No wercker.yml found and no defaults allowed.")
	}

	return ioutil.ReadFile(foundYaml)
}

// ConfigFromYaml reads a []byte as yaml and turn it into a RawConfig object
func ConfigFromYaml(file []byte) (*RawConfig, error) {
	var m RawConfig

	err := yaml.Unmarshal(file, &m)
	if err != nil {
		return nil, err
	}

	return &m, nil
}