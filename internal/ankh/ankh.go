package ankh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/jondlm/ankh/internal/util"
	"gopkg.in/yaml.v2"
)

var ConfigDir = filepath.Join(os.Getenv("HOME"), ".ankh")
var AnkhConfigPath = filepath.Join(ConfigDir, "config")
var AnkhDataDir = filepath.Join(ConfigDir, "data", fmt.Sprintf("%v", time.Now().Unix()))

// Context is a struct that represents a context for applying files to a
// Kubernetes cluster
type Context struct {
	Name            string
	KubeContext     string `yaml:"kube_context"`
	Environment     string
	ResourceProfile string `yaml:"resource_profile"`
	HelmRegistryURL string `yaml:"helm_registry_url"`
	ClusterAdmin    bool   `yaml:"cluster_admin"`
	Global          map[string]interface{}
}

// AnkhConfig defines the shape of the ~/.ankh/config file used for global
// configuration options
type AnkhConfig struct {
	CurrentContextName        string   `yaml:"current_context"` // note the intentionally offset names here
	CurrentContext            Context  // (private) filled in by code
	SupportedEnvironments     []string `yaml:"supported_environments"`
	SupportedResourceProfiles []string `yaml:"supported_resource_profiles"`
	Contexts                  map[string]Context
}

// ValidateAndInit ensures the AnkhConfig is internally sane and populates
// special fields if necessary.
func (ankhConfig *AnkhConfig) ValidateAndInit() []error {
	//selectedContext := Context{}
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

	selectedContext, contextExists := ankhConfig.Contexts[ankhConfig.CurrentContextName]

	if contextExists == false {
		errors = append(errors, fmt.Errorf("context '%s' not found in `contexts`", ankhConfig.CurrentContextName))
	} else {
		if util.Contains(ankhConfig.SupportedEnvironments, selectedContext.Environment) == false {
			errors = append(errors, fmt.Errorf("environment '%s' not found in `supported_environments`", selectedContext.Environment))
		}

		if util.Contains(ankhConfig.SupportedResourceProfiles, selectedContext.ResourceProfile) == false {
			errors = append(errors, fmt.Errorf("resource profile '%s' not found in `supported_resource_profiles`", selectedContext.ResourceProfile))
		}

		if selectedContext.HelmRegistryURL == "" {
			errors = append(errors, fmt.Errorf("missing or empty `helm_registry_url`"))
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
	}

	return errors
}

type Chart struct {
	Name    string
	Version string
	// DefaultValues are values that apply regardless of environment
	DefaultValues map[string]interface{} `yaml:"default_values"`
	// Values is a map with keys that line up with `supported_environments`
	Values map[string]interface{}
	// ResourceProfiles is a map with keys that line up with `supported_resource_profiles`
	ResourceProfiles map[string]interface{} `yaml:"resource_profiles"`
}

// Validate ensures that a chart is valid and requires a filled out AnkhConfig
// to do so
func (c *Chart) Validate(ankhConfig AnkhConfig) error {
	if c.Values != nil {
		for k := range c.Values {
			if !util.Contains(ankhConfig.SupportedEnvironments, k) {
				return fmt.Errorf("unsupported environment '%s' found in ankh file `values` for chart '%s'", k, c.Name)
			}
		}
	}

	if c.ResourceProfiles != nil {
		for k := range c.ResourceProfiles {
			if !util.Contains(ankhConfig.SupportedResourceProfiles, k) {
				return fmt.Errorf("unsupported resource profile '%s' found in ankh file `resource_profiles` for chart '%s'", k, c.Name)
			}
		}
	}

	return nil
}

// AnkhFile defines the shape of the `ankh.yaml` file which is used to define
// clusters and their contents
type AnkhFile struct {
	// (private) an absolute path to the ankh.yaml file
	Path string

	Bootstrap struct {
		Scripts []struct {
			Path string
		}
	}

	Teardown struct {
		Scripts []struct {
			Path string
		}
	}

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

	// Recursively process admin dependencies
	if config.AdminDependencies != nil {
		if config.AdminDependenciesResolved == nil {
			config.AdminDependenciesResolved = []AnkhFile{}
		}

		for _, c := range config.AdminDependencies {
			if path.IsAbs(c) == false {
				c = path.Join(filepath.Dir(config.Path), c, "ankh.yaml")
			}

			newAdminDependencyResolved, err := ProcessAnkhFile(&c)
			if err != nil {
				return config, fmt.Errorf("unable to process admin dependency: %v", err)
			}

			config.AdminDependenciesResolved = append(config.AdminDependenciesResolved, newAdminDependencyResolved)
		}

	}

	// Recursively process dependencies
	if config.Dependencies != nil {
		if config.DependenciesResovled == nil {
			config.DependenciesResovled = []AnkhFile{}
		}

		for _, c := range config.Dependencies {
			if path.IsAbs(c) == false {
				c = path.Join(filepath.Dir(config.Path), c, "ankh.yaml")
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

	ankhRcFile, err := ioutil.ReadFile(AnkhConfigPath)
	if err != nil {
		return ankhConfig, fmt.Errorf("unable to read %s file: %v", AnkhConfigPath, err)
	}

	if err := os.MkdirAll(AnkhDataDir, 0755); err != nil {
		return ankhConfig, fmt.Errorf("unable to make data dir '%s': %v", AnkhDataDir, err)
	}

	err = yaml.UnmarshalStrict(ankhRcFile, &ankhConfig)
	if err != nil {
		return ankhConfig, fmt.Errorf("unable to process %s file: %v", AnkhConfigPath, err)
	}

	errs := ankhConfig.ValidateAndInit()
	if len(errs) > 0 {
		return ankhConfig, fmt.Errorf("ankh config validation error(s):\n%s", util.MultiErrorFormat(errs))
	}

	return ankhConfig, nil
}
