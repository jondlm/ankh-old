package ankh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// CliContext is a struct that represents a context for applying files to a
// Kubernetes cluster
type CliContext struct {
	Name        string
	KubeContext string `yaml:"kube_context"`
	Environment string
	Profile     string
	HelmRepoURL string `yaml:"helm_repo_url"`
	Context     map[interface{}]interface{}
}

// CliConfig defines the shape of the .ankhrc file used for global
// configuration options
type CliConfig struct {
	CurrentContext string `yaml:"current_context"`
	Contexts       []CliContext
}

type Chart struct {
	Name    string
	Version string
	Values  map[string]interface{}
}

// Config defines the shape of the `ankh.yaml` file which is used to define
// clusters and their contents
type Config struct {
	// An absolute path to the ankh.yaml file
	Path string

	// Array of paths to other ankh.yaml files
	ChildrenPaths []string `yaml:"children_paths"`

	// Nested children. This is usually populated by looking at the
	// `ChildrenPaths` property and finding the child definitions
	Children []Config

	// The Kubernetes namespace to apply to
	Namespace string

	Deploy struct {
		Charts []Chart
	}
}

func GetConfig(filename *string) (Config, error) {
	config := Config{}
	deployFile, err := ioutil.ReadFile(fmt.Sprintf("%s", *filename))
	if err != nil {
		return config, err
	}

	err = yaml.UnmarshalStrict(deployFile, &config)
	if err != nil {
		return config, fmt.Errorf("unable to process %s file: %v", *filename, err)
	}

	// Add the absolute path of the config to the struct
	config.Path, err = filepath.Abs(*filename)
	if err != nil {
		return config, err
	}

	// Recursively process children
	if config.ChildrenPaths != nil {
		if config.Children == nil {
			config.Children = []Config{}
		}

		for _, c := range config.ChildrenPaths {
			if path.IsAbs(c) == false {
				c = path.Join(filepath.Dir(config.Path), c)
			}

			newChild, err := GetConfig(&c)
			if err != nil {
				return config, fmt.Errorf("unable to process child: %v", err)
			}

			config.Children = append(config.Children, newChild)
		}
	}

	return config, nil
}

func GetCliCurrentContext() (CliContext, error) {
	cliConfig := CliConfig{}
	context := CliContext{}

	ankhRcFile, err := ioutil.ReadFile(fmt.Sprintf("%s/.ankhrc", os.Getenv("HOME")))
	if err != nil {
		return context, fmt.Errorf("unable to read ~/.ankhrc file: %v", err)
	}

	err = yaml.UnmarshalStrict(ankhRcFile, &cliConfig)
	if err != nil {
		return context, fmt.Errorf("unable to process ~/.ankhrc file: %v", err)
	}

	for _, c := range cliConfig.Contexts {
		if c.Name == cliConfig.CurrentContext {
			context = c
			break
		}
	}

	if context.Name == "" {
		return context, fmt.Errorf("unable to locate a currentContext `%s`", cliConfig.CurrentContext)
	}

	if context.HelmRepoURL == "" {
		return context, fmt.Errorf("missing helm_repo_url from the `%s` context in ~/.ankhrc", cliConfig.CurrentContext)
	}

	return context, nil
}
