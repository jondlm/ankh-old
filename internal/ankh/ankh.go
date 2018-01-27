package ankh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/jondlm/ankh/internal/util"
	"gopkg.in/yaml.v2"
)

// Context is a struct that represents a context for applying files to a
// Kubernetes cluster
type Context struct {
	Name            string
	KubeContext     string `yaml:"kube_context"`
	Environment     string
	ResourceProfile string `yaml:"resource_profile"`
	HelmRepoURL     string `yaml:"helm_repo_url"`
	ClusterAdmin    bool   `yaml:"cluster_admin"`
	Global          map[interface{}]interface{}
}

// AnkhConfig defines the shape of the ~/.ankh/config file used for global
// configuration options
type AnkhConfig struct {
	CurrentContextName        string   `yaml:"current_context"` // note the intentionally offset names here
	CurrentContext            Context  // (private) filled in by code
	SupportedEnvironments     []string `yaml:"supported_environments"`
	SupportedResourceProfiles []string `yaml:"supported_resource_profiles"`
	Contexts                  []Context
}

// ValidateAndInit ensures the AnkhConfig is internally sane and populates
// special fields if necessary.
func (ankhConfig *AnkhConfig) ValidateAndInit() []error {
	selectedContext := Context{}
	errors := []error{}

	// Rudimentary validation here, could probably benefit from a proper
	// validation library for clarity

	if ankhConfig.CurrentContextName == "" {
		errors = append(errors, fmt.Errorf("missing or empty `current_context`"))
	}

	if ankhConfig.SupportedEnvironments == nil {
		errors = append(errors, fmt.Errorf("missing or empty `supported_environments`"))
	}

	if ankhConfig.SupportedResourceProfiles == nil {
		errors = append(errors, fmt.Errorf("missing or empty `supported_resource_profiles`"))
	}

	for _, c := range ankhConfig.Contexts {
		if c.Name == ankhConfig.CurrentContextName {
			selectedContext = c
			break
		}
	}

	if util.Contains(ankhConfig.SupportedEnvironments, selectedContext.Environment) == false {
		errors = append(errors, fmt.Errorf("environment '%s' not found in `supported_environments`", selectedContext.Environment))
	}

	if util.Contains(ankhConfig.SupportedResourceProfiles, selectedContext.ResourceProfile) == false {
		errors = append(errors, fmt.Errorf("resource profile '%s' not found in `supported_resource_profiles`", selectedContext.ResourceProfile))
	}

	if selectedContext.Name == "" {
		errors = append(errors, fmt.Errorf("unable to locate the '%s' context in your ankh config", ankhConfig.CurrentContextName))
	}

	if selectedContext.HelmRepoURL == "" {
		errors = append(errors, fmt.Errorf("missing or empty `helm_repo_url`"))
	}

	if selectedContext.KubeContext == "" {
		errors = append(errors, fmt.Errorf("missing or empty `kube_context`"))
	}

	if selectedContext.Environment == "" {
		errors = append(errors, fmt.Errorf("missing or empty `environment`"))
	}

	if selectedContext.ResourceProfile == "" {
		errors = append(errors, fmt.Errorf("missing or empty `resource_profile`"))
	}

	ankhConfig.CurrentContext = selectedContext

	return errors
}

type Chart struct {
	Name    string
	Version string
	Values  map[string]interface{}
}

// AnkhFile defines the shape of the `ankh.yaml` file which is used to define
// clusters and their contents
type AnkhFile struct {
	// (private) an absolute path to the ankh.yaml file
	Path string

	// Array of paths to other ankh.yaml files that should only be run for
	// cluster admins. This is tied to the Context.ClusterAdmin bool
	AdminDependencies []string `yaml:"admin_dependencies"`
	// (private) filled out copies of AdminDependencies
	AdminDependenciesResolved []AnkhFile `yaml:"admin_dependencies_resolved"`

	// Array of paths to other ankh.yaml files
	Dependencies []string
	// (private) filled out copies of Dependencies
	DependenciesResovled []AnkhFile `yaml:"dependencies_resolved"`

	// Nested children. This is usually populated by looking at the
	// `ChildrenPaths` property and finding the child definitions

	// The Kubernetes namespace to apply to
	Namespace string

	Charts []Chart
}

func ProcessAnkhFile(filename *string) (AnkhFile, error) {
	config := AnkhFile{}
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
	if config.Dependencies != nil {
		if config.DependenciesResovled == nil {
			config.DependenciesResovled = []AnkhFile{}
		}

		for _, c := range config.Dependencies {
			if path.IsAbs(c) == false {
				c = path.Join(filepath.Dir(config.Path), c)
			}

			newDependencyResolved, err := ProcessAnkhFile(&c)
			if err != nil {
				return config, fmt.Errorf("unable to process dependency: %v", err)
			}

			config.DependenciesResovled = append(config.DependenciesResovled, newDependencyResolved)
		}
	}

	return config, nil
}

func GetAnkhConfig() (AnkhConfig, error) {
	ankhConfig := AnkhConfig{}

	ankhRcFile, err := ioutil.ReadFile(fmt.Sprintf("%s/.ankh/config", os.Getenv("HOME")))
	if err != nil {
		return ankhConfig, fmt.Errorf("unable to read ~/.ankh/config file: %v", err)
	}

	err = yaml.UnmarshalStrict(ankhRcFile, &ankhConfig)
	if err != nil {
		return ankhConfig, fmt.Errorf("unable to process ~/.ankh/config file: %v", err)
	}

	errs := ankhConfig.ValidateAndInit()
	if len(errs) > 0 {
		return ankhConfig, fmt.Errorf("ankh config validation error(s):\n%s", util.MultiErrorFormat(errs))
	}

	return ankhConfig, nil
}
