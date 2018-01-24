package helm

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jondlm/ankh/internal/ankh"
	"github.com/jondlm/ankh/internal/util"
	// "github.com/sirupsen/logrus"
)

func templateChart(chart ankh.Chart, conf ankh.Config, ctx ankh.CliContext) (string, error) {
	helmArgs := []string{"helm", "template", "--kube-context", ctx.KubeContext, "--namespace", conf.Namespace}

	dirPath := filepath.Join(filepath.Dir(conf.Path), "charts", chart.Name)
	_, dirErr := os.Stat(dirPath)

	secretsPath := filepath.Join(filepath.Dir(conf.Path), "secrets", ctx.Environment, chart.Name+".yaml")
	_, secretsErr := os.Stat(secretsPath)
	if secretsErr == nil {
		helmArgs = append(helmArgs, "-f", secretsPath)
	}

	valuesPath := filepath.Join(filepath.Dir(conf.Path), "values", ctx.Environment, chart.Name+".yaml")
	_, valuesErr := os.Stat(valuesPath)
	if valuesErr == nil {
		helmArgs = append(helmArgs, "-f", valuesPath)
	}

	profilesPath := filepath.Join(filepath.Dir(conf.Path), "profiles", ctx.Profile, chart.Name+".yaml")
	_, profilesErr := os.Stat(profilesPath)
	if profilesErr == nil {
		helmArgs = append(helmArgs, "-f", profilesPath)
	}

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

	// Check if Context is not empty
	if ctx.Context != nil {
		for _, item := range util.Collapse(ctx.Context, nil, nil) {
			helmArgs = append(helmArgs, "--set", "context."+item)
		}
	}

	tarballFileName := fmt.Sprintf("%s-%s.tgz", chart.Name, chart.Version)
	tarballPath := filepath.Join(filepath.Dir(conf.Path), "charts", tarballFileName)
	tarballURL := fmt.Sprintf("%s/%s", strings.TrimRight(ctx.HelmRepoURL, "/"), tarballFileName)

	if dirErr == nil {
		helmArgs = append(helmArgs, dirPath)
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
		defer resp.Body.Close()

		io.Copy(f, resp.Body)

		helmArgs = append(helmArgs, tarballPath)
	}

	helmCmd := exec.Command(helmArgs[0], helmArgs[1:]...)
	helmOutput, err := helmCmd.Output()

	if err != nil {
		return "", fmt.Errorf("error running the helm command: %v", err)
	}

	return string(helmOutput), nil
}

func Template(conf ankh.Config, ctx ankh.CliContext) (string, error) {
	chartOutputCombined := ""

	if len(conf.Deploy.Charts) > 0 {
		for _, chart := range conf.Deploy.Charts {
			chartOutput, err := templateChart(chart, conf, ctx)
			if err != nil {
				return chartOutputCombined, err
			}
			chartOutputCombined += chartOutput
		}
	}

	if conf.Children == nil {
		return chartOutputCombined, nil
	}

	childOutputCombined := ""
	for _, childConfig := range conf.Children {
		childOutput, err := Template(childConfig, ctx)
		if err != nil {
			return childOutputCombined, err
		}
		childOutputCombined += childOutput
	}

	return chartOutputCombined + childOutputCombined, nil
}
