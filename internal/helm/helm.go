package helm

import (
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"

	"github.com/jondlm/ankh/internal/ankh"
	"github.com/jondlm/ankh/internal/util"
)

type action string

const (
	Apply  action = "apply"
	Delete action = "delete"
)

func Execute(act action, conf ankh.Config, ctx ankh.CliContext, log *logrus.Logger) error {
	if len(conf.Deploy.Charts) > 0 {
		for _, chart := range conf.Deploy.Charts {
			log.WithFields(logrus.Fields{
				"name": chart.Name,
				"path": conf.Path,
			}).Info("starting chart")

			helmArgs := []string{"helm", "template", "--kube-context", ctx.KubeContext, "--namespace", conf.Namespace}
			kubectlArgs := []string{"kubectl", string(act), "--context", ctx.KubeContext, "--namespace", conf.Namespace, "-f", "-"}

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
			tarballURL := fmt.Sprintf("%s/%s", ctx.HelmRepoURL, tarballFileName)

			if dirErr == nil {
				helmArgs = append(helmArgs, dirPath)
			} else {
				f, err := os.Create(tarballPath)
				if err != nil {
					return err
				}
				defer f.Close()

				// TODO: this code should be modified to properly fetch charts
				tr := &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				}
				client := &http.Client{Transport: tr}
				resp, err := client.Get(tarballURL)
				if err != nil {
					return err
				}
				if resp.StatusCode != 200 {
					return fmt.Errorf("got a status code %v when trying to call %s", resp.StatusCode, tarballURL)
				}
				defer resp.Body.Close()

				io.Copy(f, resp.Body)

				helmArgs = append(helmArgs, tarballPath)
			}

			helmCmd := exec.Command(helmArgs[0], helmArgs[1:]...)
			helmOutput, err := helmCmd.CombinedOutput()

			if err != nil {
				return fmt.Errorf("error running the helm command: %v", err)
			}

			kubectlCmd := exec.Command(kubectlArgs[0], kubectlArgs[1:]...)

			kubectlStdoutPipe, _ := kubectlCmd.StdoutPipe()
			kubectlStderrPipe, _ := kubectlCmd.StderrPipe()
			kubectlStdinPipe, _ := kubectlCmd.StdinPipe()

			kubectlCmd.Start()
			kubectlStdinPipe.Write([]byte(helmOutput))
			kubectlStdinPipe.Close()

			kubectlOut, _ := ioutil.ReadAll(kubectlStdoutPipe)
			kubectlErr, _ := ioutil.ReadAll(kubectlStderrPipe)

			err = kubectlCmd.Wait()
			if err != nil {
				log.Errorf("error running the kubectl command:\n%s", kubectlErr)
			} else {
				log.Info(string(kubectlOut))
			}
		}
	}

	if conf.Children != nil {
		for _, childConfig := range conf.Children {
			err := Execute(Apply, childConfig, ctx, log)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
