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
	//"github.com/davecgh/go-spew/spew"
)

func templateChart(log *logrus.Logger, chart ankh.Chart, ankhFile ankh.AnkhFile, ankhConfig ankh.AnkhConfig) (string, error) {
	ctx := ankhConfig.CurrentContext
	helmArgs := []string{"helm", "template", "--kube-context", ctx.KubeContext, "--namespace", ankhFile.Namespace}

	dirPath := filepath.Join(filepath.Dir(ankhFile.Path), "charts", chart.Name)
	_, dirErr := os.Stat(dirPath)

	var tag string
	if chart.Values != nil {
		switch x := chart.Values["tag"].(type) {
		case string:
			tag = x
		default:
			tag = ""
		}
	}

	if tag != "" {
		helmArgs = append(helmArgs, "--set", "tag="+tag)
	}

	// Check if Global is not empty
	if ctx.Global != nil {
		for _, item := range util.Collapse(ctx.Global, nil, nil) {
			helmArgs = append(helmArgs, "--set", "context."+item)
		}
	}

	tarballFileName := fmt.Sprintf("%s-%s.tgz", chart.Name, chart.Version)
	tarballPath := filepath.Join(filepath.Dir(ankhFile.Path), "charts", tarballFileName)
	tarballURL := fmt.Sprintf("%s/%s", strings.TrimRight(ctx.HelmRepoURL, "/"), tarballFileName)

	// Setup a temporary director where we'll either copy the chart files, if
	// we've got a directory, or we'll download and extract a tarball to the temp
	// dir. Then we'll mutate some of the ankh specific files based on the
	// current environment and resource profile. Then we'll use those files as
	// arguments to the helm command.
	tmpDir, err := ioutil.TempDir(os.TempDir(), chart.Name)
	if err != nil {
		return "", err
	}

	// if we already have a dir, let's just copy it to a temp directory so we can
	// make changes to the ankh specific yaml files before passing them as `-f`
	// args to `helm template`
	if dirErr == nil {
		if err := util.CopyDir(dirPath, filepath.Join(tmpDir, chart.Name)); err != nil {
			return "", err
		}
	} else {
		f, err := os.Create(tarballPath)
		if err != nil {
			return "", err
		}
		defer f.Close()

		// TODO: this code should be modified to properly fetch charts
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

		if err = util.Untar(tmpDir, resp.Body); err != nil {
			return "", err
		}
	}

	chartPath := filepath.Join(tmpDir, chart.Name)
	valuesPath := filepath.Join(chartPath, "ankh-values.yaml")
	valuesAltPath := filepath.Join(filepath.Dir(ankhFile.Path), "values", ctx.Environment, chart.Name+".yaml")
	resourceProfilesPath := filepath.Join(chartPath, "ankh-resource-profiles.yaml")
	resourceProfilesAltPath := filepath.Join(filepath.Dir(ankhFile.Path), "resource-profiles", ctx.ResourceProfile, chart.Name+".yaml")

	// TODO: load secrets from another repo, eventually from vault
	// secretsPath := filepath.Join(filepath.Dir(ankhFile.Path), "secrets", chart.Name+".yaml")
	// _, secretsErr := os.Stat(secretsPath)
	// if secretsErr == nil {
	// 	helmArgs = append(helmArgs, "-f", secretsPath)
	// }

	_, valuesErr := os.Stat(valuesPath)
	if valuesErr == nil {
		if err := mutateYAMLFile(valuesPath, ctx.Environment, ankhConfig.SupportedEnvironments); err != nil {
			return "", fmt.Errorf("unable to process ankh-values.yaml file for chart '%s': %v", chart.Name, err)
		}
		helmArgs = append(helmArgs, "-f", valuesPath)
	}

	_, valuesAltErr := os.Stat(valuesAltPath)
	if valuesAltErr == nil {
		helmArgs = append(helmArgs, "-f", valuesAltPath)
	}

	_, resourceProfilesError := os.Stat(resourceProfilesPath)
	if resourceProfilesError == nil {
		if err := mutateYAMLFile(resourceProfilesPath, ctx.ResourceProfile, ankhConfig.SupportedResourceProfiles); err != nil {
			return "", fmt.Errorf("unable to process ankh-resource-profiles.yaml file for chart '%s': %v", chart.Name, err)
		}
		helmArgs = append(helmArgs, "-f", resourceProfilesPath)
	}

	_, resourceProfilesAltErr := os.Stat(resourceProfilesAltPath)
	if resourceProfilesAltErr == nil {
		helmArgs = append(helmArgs, "-f", resourceProfilesAltPath)
	}

	helmArgs = append(helmArgs, chartPath)

	fmt.Println(helmArgs)

	helmCmd := exec.Command(helmArgs[0], helmArgs[1:]...)
	helmOutput, err := helmCmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("error running the helm command: %s", helmOutput)
	}

	return string(helmOutput), nil
}

func mutateYAMLFile(filename, key string, supportedKeys []string) error {
	in := make(map[string]interface{})

	inBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(inBytes, &in); err != nil {
		return err
	}

	out := make(map[interface{}]interface{})

	for k, _ := range in {
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
	chartOutputCombined := ""

	if len(ankhFile.Charts) > 0 {
		for _, chart := range ankhFile.Charts {
			log.Debugf("templating %s", chart.Name)
			chartOutput, err := templateChart(log, chart, ankhFile, ankhConfig)
			if err != nil {
				return chartOutputCombined, err
			}
			chartOutputCombined += chartOutput
		}
	}

	if ankhFile.DependenciesResovled == nil {
		return chartOutputCombined, nil
	}

	childOutputCombined := ""
	for _, childConfig := range ankhFile.DependenciesResovled {
		childOutput, err := Template(log, childConfig, ankhConfig)
		if err != nil {
			return childOutputCombined, err
		}
		childOutputCombined += childOutput
	}

	return chartOutputCombined + childOutputCombined, nil
}
