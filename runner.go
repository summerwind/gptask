package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed prompt.txt
var systemContent string

var (
	errInvalidFormat = errors.New("invalid format")
	errInvalidAction = errors.New("invalid action")
)

const (
	FeedbackSuccess  = "Success"
	FeedbackContinue = "Continue"
)

type Config struct {
	APIKey   string
	Model    string
	WorkDir  string
	MaxSteps int
	Verbose  bool
}

type Command struct {
	Task     string `json:"task,omitempty"`
	Thought  string `json:"thought,omitempty"`
	Action   string `json:"action,omitempty"`
	Input    string `json:"input,omitempty"`
	Feedback string `json:"feedback,omitempty"`
}

type FileCommandInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Runner struct {
	Config  *Config
	Session *Session
	WorkDir string
}

func NewRunner(c *Config) *Runner {
	return &Runner{
		Config:  c,
		Session: NewSession(c),
	}
}

func (r *Runner) Run(task string) error {
	var done bool

	ctx := context.Background()
	msg := task
	numStep := 1

	for {
		reply, err := r.Session.Send(ctx, msg)
		if err != nil {
			return err
		}

		if reply == "" {
			r.vlog("Empty reply - raw:\n%s", reply)
			r.Session.Rewind()
			continue
		}

		cmd, err := decodeCommand(reply)
		if err != nil {
			return err
		}

		if cmd.Thought == "" {
			r.vlog("No thought - raw:\n%s", reply)
			r.Session.Rewind()
			continue
		}

		fmt.Printf("Step %d: %s\n", numStep, cmd.Thought)
		fmt.Printf("Action: %s\n", cmd.Action)
		if cmd.Input != "" {
			fmt.Printf("%s\n", cmd.Input)
		}

		if cmd.Action == "done" {
			done = true
			break
		}

		feedback, err := r.runCommand(cmd)
		if err != nil {
			if errors.Is(err, errInvalidAction) {
				r.vlog("Invalid action - raw:\n%s", reply)
				r.Session.Rewind()
				continue
			}
			return fmt.Errorf("failed to run command: %v", err)
		}
		fmt.Printf("%s\n\n", feedback)

		numStep += 1
		if numStep > r.Config.MaxSteps {
			break
		}
		msg = fmt.Sprintf("feedback:\n```\n%s\n```\n", feedback)
	}

	if !done {
		fmt.Println("The maximum number of steps has been reached")
	}

	return nil
}

func (r *Runner) runCommand(cmd Command) (string, error) {
	var (
		feedback string
		err      error
	)

	switch cmd.Action {
	case "file":
		feedback, err = r.runFileCommand(cmd)
	case "python":
		feedback, err = r.runPythonCommand(cmd)
	case "shell":
		feedback, err = r.runShellCommand(cmd)
	case "workdir":
		feedback, err = r.runWorkDirCommand(cmd)
	default:
		err = errInvalidAction
	}

	if err != nil {
		return "", err
	}

	return strings.Trim(feedback, "\n"), nil
}

func (r *Runner) runFileCommand(cmd Command) (string, error) {
	var input FileCommandInput

	err := yaml.Unmarshal([]byte(cmd.Input), &input)
	if err != nil {
		return "", err
	}

	if input.Path == "" {
		return "file path must be specified", nil
	}

	if !filepath.IsAbs(input.Path) {
		input.Path = filepath.Join(r.getWorkDir(), input.Path)
	}

	dirPath := filepath.Dir(input.Path)
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		return err.Error(), nil
	}

	err = os.WriteFile(input.Path, []byte(input.Content), 0644)
	if err != nil {
		return err.Error(), nil
	}

	return FeedbackSuccess, nil
}

func (r *Runner) runPythonCommand(cmd Command) (string, error) {
	python := exec.Command("python3", "-c", cmd.Input)
	python.Dir = r.getWorkDir()

	output, err := python.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(output), nil

		}
		return "", err
	}

	return string(output), nil
}

func (r *Runner) runShellCommand(cmd Command) (string, error) {
	var (
		output []byte
		err    error
	)

	shell := exec.Command("bash", "-e", "-o", "pipefail", "-c", cmd.Input)
	shell.Dir = r.getWorkDir()

	output, err = shell.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(exitErr.Stderr), nil
		}
		return "", err
	}

	err = os.WriteFile("output.log", output, 0644)
	if err != nil {
		return "", err
	}

	if len(output) == 0 {
		return FeedbackSuccess, nil
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
		lines = append(lines, FeedbackSuccess)
		return strings.Join(lines, "\n"), nil
	}

	return string(output), nil
}

func (r *Runner) runWorkDirCommand(cmd Command) (string, error) {
	workDir := filepath.Clean(cmd.Input)
	if !filepath.IsAbs(workDir) {
		workDir = filepath.Join(r.Config.WorkDir, workDir)
	}

	if !strings.HasPrefix(workDir, r.Config.WorkDir) {
		return "unauthorized path.", nil
	}

	r.WorkDir = workDir

	return FeedbackSuccess, nil
}

func (r *Runner) getWorkDir() string {
	if r.WorkDir == "" {
		return r.Config.WorkDir
	}
	return r.WorkDir
}

func (r *Runner) vlog(format string, v ...any) {
	log.Printf(format, v...)
}

func decodeCommand(reply string) (Command, error) {
	var (
		cmd   Command
		block bool
		i     int
	)

	lines := strings.Split(reply, "\n")
	lineLen := len(lines)
	input := []string{}

	for i < lineLen {
		switch {
		case strings.HasPrefix(lines[i], "thought:"):
			parts := strings.SplitN(lines[i], ":", 2)
			cmd.Thought = strings.Trim(parts[1], "\n ")
		case strings.HasPrefix(lines[i], "action:"):
			parts := strings.SplitN(lines[i], ":", 2)
			cmd.Action = strings.Trim(parts[1], "\n ")
		case strings.HasPrefix(lines[i], "input:"):
			i += 1
			for i < lineLen {
				if strings.HasPrefix(lines[i], "```") {
					if !block {
						block = true
						i += 1
						continue
					}
					break
				}
				if block {
					input = append(input, lines[i])
				}

				i += 1
			}
			cmd.Input = strings.Join(input, "\n")
		}

		i += 1
	}

	return cmd, nil
}

func encodeCommand(cmd Command) string {
	var lines []string

	switch {
	case cmd.Task != "":
		lines = append(lines, fmt.Sprintf("task:\n```\n%s\n```", strings.Trim(cmd.Task, "\n")))
	case cmd.Thought != "":
		lines = append(lines, fmt.Sprintf("thought: %s", cmd.Thought))
		lines = append(lines, fmt.Sprintf("action: %s", cmd.Action))
		lines = append(lines, fmt.Sprintf("input:\n```\n%s\n```", strings.Trim(cmd.Input, "\n")))
	case cmd.Feedback != "":
		lines = append(lines, fmt.Sprintf("feedback:\n```\n%s\n```", strings.Trim(cmd.Feedback, "\n")))
	}

	return strings.Join(lines, "\n")
}
