package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sashabaranov/go-openai"
)

//go:embed prompt.txt
var systemContent string

var errInvalidFormat = errors.New("invalid format")

type Config struct {
	APIKey   string
	Model    string
	WorkDir  string
	MaxSteps int
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

	ctx := context.Background()

	messages := []openai.ChatCompletionMessage{
		{Role: "system", Content: systemContent},
		{Role: "user", Content: task},
	}

	for i := 0; i < r.Config.MaxSteps; i++ {
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
		cmd, err := decodeCommand(reply)
		if err != nil {
			return err
		}

		if cmd.Thought == "" {
			done = true
			break
		}

		fmt.Printf("Step %d: %s\n", i+1, cmd.Thought)
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
			return fmt.Errorf("failed to run command: %v", err)
		}

		fmt.Printf("%s\n\n", feedback)

		messages = append(messages, []openai.ChatCompletionMessage{
			{Role: "assistant", Content: encodeCommand(cmd)},
			{Role: "user", Content: encodeCommand(Command{Feedback: feedback})},
		}...)
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
		return "", fmt.Errorf("invalid action: %s", cmd.Action)
	}
}

func (r *Runner) runFileCommand(cmd Command) (string, error) {
	lines := strings.SplitN(cmd.Input, "\n", 2)
	if len(lines) < 2 {
		return "file path and content must be specified to create a file", nil
	}

	targetPath := filepath.Join(r.Config.WorkDir, lines[0])

	err := os.WriteFile(targetPath, []byte(lines[1]), 0644)
	if err != nil {
		return err.Error(), nil
	}

	return "success", nil
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

	lines := strings.Split(cmd.Input, "\n")

	for _, line := range lines {
		shell := exec.Command("bash", "-c", line)
		shell.Dir = r.Config.WorkDir

		output, err = shell.CombinedOutput()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return string(output), nil

			}
			return "", err
		}
	}

	return string(output), nil
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
			for i < len(lines) {
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
