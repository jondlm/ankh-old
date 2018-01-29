package helm

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jondlm/ankh/internal/ankh"
	"github.com/jondlm/ankh/internal/util"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

func templateChart(log *logrus.Logger, chart ankh.Chart, ankhFile ankh.AnkhFile, ankhConfig ankh.AnkhConfig) (string, error) {
	ctx := ankhConfig.CurrentContext
	helmArgs := []string{"helm", "template", "--kube-context", ctx.KubeContext, "--namespace", ankhFile.Namespace}

	dirPath := filepath.Join(filepath.Dir(ankhFile.Path), "charts", chart.Name)
	_, dirErr := os.Stat(dirPath)

	// Setup a directory where we'll either copy the chart files, if we've got a
	// directory, or we'll download and extract a tarball to the temp dir. Then
	// we'll mutate some of the ankh specific files based on the current
	// environment and resource profile. Then we'll use those files as arguments
	// to the helm command.
	tmpDir, err := ioutil.TempDir(ankh.AnkhDataDir, chart.Name+"-")
	if err != nil {
		return "", err
	}

	if chart.DefaultValues != nil {
		defaultValuesPath := filepath.Join(tmpDir, "default-values.yaml")
		defaultValuesBytes, err := yaml.Marshal(chart.DefaultValues)
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(defaultValuesPath, defaultValuesBytes, 0644); err != nil {
			return "", err
		}

		helmArgs = append(helmArgs, "-f", defaultValuesPath)
	}

	if chart.Values != nil && chart.Values[ctx.Environment] != nil {
		valuesPath := filepath.Join(tmpDir, "values.yaml")
		valuesBytes, err := yaml.Marshal(chart.Values[ctx.Environment])
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(valuesPath, valuesBytes, 0644); err != nil {
			return "", err
		}

		helmArgs = append(helmArgs, "-f", valuesPath)
	}

	if chart.ResourceProfiles != nil && chart.ResourceProfiles[ctx.ResourceProfile] != nil {
		resourceProfilesPath := filepath.Join(tmpDir, "resource-profiles.yaml")
		resourceProfilesBytes, err := yaml.Marshal(chart.ResourceProfiles[ctx.ResourceProfile])
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(resourceProfilesPath, resourceProfilesBytes, 0644); err != nil {
			return "", err
		}

		helmArgs = append(helmArgs, "-f", resourceProfilesPath)
	}

	// Check if Global contains anything
	if ctx.Global != nil {
		for _, item := range util.Collapse(ctx.Global, nil, nil) {
			helmArgs = append(helmArgs, "--set", "global."+item)
		}
	}

	tarballFileName := fmt.Sprintf("%s-%s.tgz", chart.Name, chart.Version)
	tarballPath := filepath.Join(filepath.Dir(ankhFile.Path), "charts", tarballFileName)
	tarballURL := fmt.Sprintf("%s/%s", strings.TrimRight(ctx.HelmRegistryURL, "/"), tarballFileName)

	// if we already have a dir, let's just copy it to a temp directory so we can
	// make changes to the ankh specific yaml files before passing them as `-f`
	// args to `helm template`
	if dirErr == nil {
		if err := util.CopyDir(dirPath, filepath.Join(tmpDir, chart.Name)); err != nil {
			return "", err
		}
	} else {
		log.Debugf("ensuring chart directory is made at %s", filepath.Dir(tarballPath))
		if err := os.MkdirAll(filepath.Dir(tarballPath), 0755); err != nil {
			return "", err
		}

		log.Debugf("opening system file at %s", tarballPath)
		f, err := os.Create(tarballPath)
		if err != nil {
			return "", err
		}
		defer f.Close()

		// TODO: this code should be modified to properly fetch charts
		log.Debugf("downloading chart from %s", tarballURL)
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		resp, err := client.Get(tarballURL)
		if err != nil {
			return "", err
		}
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("got a status code %v when trying to call %s", resp.StatusCode, tarballURL)
		}
		// defer resp.Body.Close()

		log.Debugf("untarring chart to %s", tmpDir)
		if err = util.Untar(tmpDir, resp.Body); err != nil {
			return "", err
		}
	}

	chartPath := filepath.Join(tmpDir, chart.Name)
	valuesPath := filepath.Join(chartPath, "ankh-values.yaml")
	resourceProfilesPath := filepath.Join(chartPath, "ankh-resource-profiles.yaml")

	// TODO: load secrets from another repo, eventually from vault
	// secretsPath := filepath.Join(filepath.Dir(ankhFile.Path), "secrets", chart.Name+".yaml")
	// _, secretsErr := os.Stat(secretsPath)
	// if secretsErr == nil {
	// 	helmArgs = append(helmArgs, "-f", secretsPath)
	// }

	_, valuesErr := os.Stat(valuesPath)
	if valuesErr == nil {
		if err := createReducedYAMLFile(valuesPath, ctx.Environment, ankhConfig.SupportedEnvironments); err != nil {
			return "", fmt.Errorf("unable to process ankh-values.yaml file for chart '%s': %v", chart.Name, err)
		}
		helmArgs = append(helmArgs, "-f", valuesPath)
	}

	_, resourceProfilesError := os.Stat(resourceProfilesPath)
	if resourceProfilesError == nil {
		if err := createReducedYAMLFile(resourceProfilesPath, ctx.ResourceProfile, ankhConfig.SupportedResourceProfiles); err != nil {
			return "", fmt.Errorf("unable to process ankh-resource-profiles.yaml file for chart '%s': %v", chart.Name, err)
		}
		helmArgs = append(helmArgs, "-f", resourceProfilesPath)
	}

	helmArgs = append(helmArgs, chartPath)

	log.Debugf("running helm command %s", strings.Join(helmArgs, " "))

	helmCmd := exec.Command(helmArgs[0], helmArgs[1:]...)
	helmOutput, err := helmCmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("error running the helm command: %s", helmOutput)
	}

	return string(helmOutput), nil
}

func createReducedYAMLFile(filename, key string, supportedKeys []string) error {
	in := make(map[string]interface{})

	inBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(inBytes, &in); err != nil {
		return err
	}

	out := make(map[interface{}]interface{})

	for k := range in {
		if util.Contains(supportedKeys, k) == false {
			return fmt.Errorf("unsupported key `%s` found", k)
		}
	}

	if in[key] == nil {
		return fmt.Errorf("missing `%s` key", key)
	}

	switch t := in[key].(type) {
	case map[interface{}]interface{}:
		for k, v := range t {
			// TODO: using `.(string)` here could cause a panic in cases where the
			// key isn't a string, which is pretty uncommon

			// TODO: validate
			out[k.(string)] = v
		}
	default:
		out[key] = in[key]
	}

	outBytes, err := yaml.Marshal(&out)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filename, outBytes, 0644); err != nil {
		return err
	}

	return nil
}

func Template(log *logrus.Logger, ankhFile ankh.AnkhFile, ankhConfig ankh.AnkhConfig) (string, error) {
	adminDepOutputCombined := ""
	depOutputCombined := ""
	chartOutputCombined := ""

	log.Debugf("beginning templating of %s", ankhFile.Path)

	if ankhFile.AdminDependenciesResolved != nil && ankhConfig.CurrentContext.ClusterAdmin == true {
		log.Debugf("templating admin deps")
		for _, adminDepConfig := range ankhFile.AdminDependenciesResolved {
			adminDepOutput, err := Template(log, adminDepConfig, ankhConfig)
			if err != nil {
				return adminDepOutputCombined, err
			}
			adminDepOutputCombined += adminDepOutput
		}
	}

	if ankhFile.DependenciesResovled != nil {
		log.Debugf("templating deps")
		for _, dependencyConfig := range ankhFile.DependenciesResovled {
			depOutput, err := Template(log, dependencyConfig, ankhConfig)
			if err != nil {
				return depOutputCombined, err
			}
			depOutputCombined += depOutput
		}
	}

	if len(ankhFile.Charts) > 0 {
		log.Debugf("templating charts")
		for _, chart := range ankhFile.Charts {
			log.Debugf("templating chart '%s'", chart.Name)

			if err := chart.Validate(ankhConfig); err != nil {
				return chartOutputCombined, err
			}

			chartOutput, err := templateChart(log, chart, ankhFile, ankhConfig)
			if err != nil {
				return chartOutputCombined, err
			}
			chartOutputCombined += chartOutput
		}
	}

	return adminDepOutputCombined + depOutputCombined + chartOutputCombined, nil
}
