package kubectl

import (
	"fmt"
	"io/ioutil"
	"os/exec"

	"github.com/jondlm/ankh/internal/ankh"
)

type action string

const (
	Apply  action = "apply"
	Delete action = "delete"
)

func Execute(act action, input string, conf ankh.Config, ctx ankh.CliContext) (string, error) {

	kubectlArgs := []string{"kubectl", string(act), "--context", ctx.KubeContext, "--namespace", conf.Namespace, "-f", "-"}

	kubectlCmd := exec.Command(kubectlArgs[0], kubectlArgs[1:]...)

	kubectlStdoutPipe, _ := kubectlCmd.StdoutPipe()
	kubectlStderrPipe, _ := kubectlCmd.StderrPipe()
	kubectlStdinPipe, _ := kubectlCmd.StdinPipe()

	kubectlCmd.Start()
	kubectlStdinPipe.Write([]byte(input))
	kubectlStdinPipe.Close()

	kubectlOut, _ := ioutil.ReadAll(kubectlStdoutPipe)
	kubectlErr, _ := ioutil.ReadAll(kubectlStderrPipe)

	err := kubectlCmd.Wait()
	if err != nil {
		return "", fmt.Errorf("error running the kubectl command:\n%s", kubectlErr)
	}
	return string(kubectlOut), nil
}
