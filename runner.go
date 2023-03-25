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

	"github.com/sashabaranov/go-openai"
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

type Runner struct {
	Config *Config
	Client *openai.Client
}

func NewRunner(c *Config) *Runner {
	return &Runner{
		Config: c,
		Client: openai.NewClient(c.APIKey),
	}
}

func (r *Runner) Run(task string) error {
	var done bool
	var numStep int = 1

	ctx := context.Background()

	messages := []openai.ChatCompletionMessage{
		{Role: "system", Content: systemContent},
		{Role: "user", Content: task},
	}

	for {
		req := openai.ChatCompletionRequest{
			Model:       r.Config.Model,
			Temperature: 0.0,
			Stop:        []string{"feedback:"},
			Messages:    messages,
		}

		res, err := r.Client.CreateChatCompletion(ctx, req)
		if err != nil {
			return fmt.Errorf("API error: %v", err)
		}

		reply := res.Choices[0].Message.Content
		if reply == "" {
			r.vlog("empty reply. continue.")
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    "user",
				Content: FeedbackContinue,
			})
			continue
		}

		cmd, err := decodeCommand(reply)
		if err != nil {
			return err
		}

		if cmd.Thought == "" {
			r.vlog("no thought found. raw reply:\n%s", reply)
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
				r.vlog("%s. try again.", err.Error())
				continue
			}
			return fmt.Errorf("failed to run command: %v", err)
		}
		feedback = strings.Trim(feedback, "\n")
		fmt.Printf("%s\n\n", feedback)

		messages = append(messages, []openai.ChatCompletionMessage{
			{Role: "assistant", Content: encodeCommand(cmd)},
			{Role: "user", Content: encodeCommand(Command{Feedback: feedback})},
		}...)

		numStep += 1
		if numStep > r.Config.MaxSteps {
			break
		}
	}

	if !done {
		fmt.Println("The maximum number of steps has been reached")
	}

	return nil
}

func (r *Runner) runCommand(cmd Command) (string, error) {
	switch cmd.Action {
	case "file":
		return r.runFileCommand(cmd)
	case "python":
		return r.runPythonCommand(cmd)
	case "shell":
		return r.runShellCommand(cmd)
	default:
		return "", errInvalidAction
	}
}

func (r *Runner) runFileCommand(cmd Command) (string, error) {
	lines := strings.SplitN(cmd.Input, "\n", 2)
	if len(lines) == 0 {
		return "file path and content must be specified to create a file", nil
	}
	if len(lines) == 1 {
		lines = append(lines, "")
	}

	targetPath := lines[0]
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(r.Config.WorkDir, lines[0])
	}

	err := os.WriteFile(targetPath, []byte(lines[1]), 0644)
	if err != nil {
		return err.Error(), nil
	}

	return FeedbackSuccess, nil
}

func (r *Runner) runPythonCommand(cmd Command) (string, error) {
	python := exec.Command("python3", "-c", cmd.Input)
	python.Dir = r.Config.WorkDir

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
	shell.Dir = r.Config.WorkDir

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
		lines.append(["...(snip)..."], lines...)
		return strings.Join(lines, "\n"), nil
	}

	return string(output), nil
}

func (r *Runner) vlog(format string, v ...any) {
	log.Printf(format, v...)
}

func decodeCommand(reply string) (Command, error) {
	var cmd Command

	lines := strings.Split(reply, "\n")

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "thought:"):
			parts := strings.SplitN(line, ":", 2)
			cmd.Thought = strings.Trim(parts[1], "\n ")
		case strings.HasPrefix(line, "action:"):
			parts := strings.SplitN(line, ":", 2)
			cmd.Action = strings.Trim(parts[1], "\n ")
		case strings.HasPrefix(line, "input:"):
			input := []string{}

			start := false
			for i < len(lines)-1 {
				if strings.HasPrefix(lines[i+1], "```") {
					if start {
						break
					}

					start = true
					i += 1
					continue
				}

				input = append(input, lines[i+1])
				i += 1
			}

			cmd.Input = strings.Join(input, "\n")
			break
		}
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
