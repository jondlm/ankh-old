package main

import (
	"fmt"
	"os"

	//"github.com/davecgh/go-spew/spew"
	"github.com/jawher/mow.cli"
	"github.com/sirupsen/logrus"

	"github.com/jondlm/ankh/internal/ankh"
	"github.com/jondlm/ankh/internal/helm"
	"github.com/jondlm/ankh/internal/kubectl"
)

var log = logrus.New()

func main() {
	formatter := logrus.TextFormatter{
		DisableTimestamp: true,
	}
	log.Out = os.Stdout
	log.Level = logrus.DebugLevel
	log.Formatter = &formatter

	app := cli.App("ankh", "AppNexus Kubernetes Helper")
	app.Spec = ""

	app.Command("apply", "Deploy an ankh file to a kubernetes cluster", func(cmd *cli.Cmd) {

		cmd.Spec = "[-f]"

		var (
			filename = cmd.StringOpt("f filename", "ankh.yaml", "Config file name")
		)

		cmd.Action = func() {
			ankhConfig, err := ankh.GetAnkhConfig()
			check(err)

			config, err := ankh.ProcessAnkhFile(filename)
			check(err)

			helmOutput, err := helm.Template(log, config, ankhConfig)
			check(err)

			action := kubectl.Apply
			log.Info("starting kubectl")
			kubectlOutput, err := kubectl.Execute(action, helmOutput, config, ankhConfig)
			check(err)

			fmt.Println(kubectlOutput)

			log.Info(helmOutput)
			log.Info("complete")
			os.Exit(0)
		}
	})

	app.Command("template", "Output the results of templating an ankh file", func(cmd *cli.Cmd) {

		cmd.Spec = "[-f]"

		var (
			filename = cmd.StringOpt("f filename", "ankh.yaml", "Config file name")
		)

		cmd.Action = func() {
			ankhConfig, err := ankh.GetAnkhConfig()
			check(err)

			config, err := ankh.ProcessAnkhFile(filename)
			check(err)

			log.Info("starting helm template")
			helmOutput, err := helm.Template(log, config, ankhConfig)
			check(err)

			fmt.Println(helmOutput)
			log.Info("complete")
			os.Exit(0)
		}
	})

	app.Run(os.Args)
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
