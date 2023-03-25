package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	_version = "dev"
	_commit  = "HEAD"
)

func main() {
	var c Config

	var cmd = &cobra.Command{
		Use:     "gptask",
		Short:   "A command-line tool for executing tasks from natural language using GPT.",
		Args:    cobra.ExactArgs(1),
		Version: fmt.Sprintf("%s (%s)", _version, _commit),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(&c, args[0])
		},
	}

	pflag := cmd.Flags()
	pflag.StringVarP(&c.Model, "model", "m", "gpt-3.5-turbo-0301", "Name of the GPT model to use")
	pflag.StringVarP(&c.WorkDir, "workdir", "w", "/opt/gptask", "Working directory")
	pflag.IntVar(&c.MaxSteps, "max-steps", 10, "Maximum number of steps the task is allowed to take")
	pflag.BoolVar(&c.Verbose, "verbose", false, "Output verbose log")

	cmd.PersistentFlags().Bool("help", false, "Display this help and exit")
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(c *Config, task string) error {
	c.APIKey = os.Getenv("OPENAI_API_KEY")
	if c.APIKey == "" {
		return errors.New("Enrironment variable 'OPENAI_API_KEY' must be set")
	}

	runner := NewRunner(c)

	return runner.Run(task)
}
