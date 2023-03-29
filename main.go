package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
)

var (
	_version = "dev"
	_commit  = "HEAD"
)

var dl *log.Logger

func main() {
	var (
		c   Config
		dlp string
	)

	var cmd = &cobra.Command{
		Use:     "gptask",
		Short:   "A command-line tool for executing tasks from natural language using GPT.",
		Args:    cobra.ExactArgs(1),
		Version: fmt.Sprintf("%s (%s)", _version, _commit),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dlp != "" {
				f, err := os.OpenFile(dlp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					return err
				}
				defer f.Close()

				dl = log.New(f, "", log.LstdFlags)
			}
			return run(&c, args[0])
		},
	}

	pflag := cmd.Flags()
	pflag.StringVarP(&c.Model, "model", "m", "gpt-3.5-turbo-0301", "Name of the GPT model to use")
	pflag.StringVarP(&c.WorkDir, "workdir", "w", "/opt/gptask", "Working directory")
	pflag.IntVar(&c.MaxSteps, "max-steps", 10, "Maximum number of steps the task is allowed to take")
	pflag.StringVar(&dlp, "debug", "", "Write debug log to a file")

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

func debugLog(msg, content string) {
	if dl == nil {
		return
	}

	if content == "" {
		dl.Printf("%s\n", msg)
	} else {
		dl.Printf("%s\n%s\n", msg, content)
	}
}
