package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
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
	Thought  string `json:"thought,omitempty"`
	Action   string `json:"action,omitempty"`
	Input    string `json:"input,omitempty"`
	Feedback string `json:"feedback,omitempty"`
}

type FileCommandInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ChangeDirCommandInput struct {
	Dir string `json:"dir"`
}

type SearchCommandInput struct {
	Query string `json:"query"`
}

type SearchResult struct {
	Title string
	Desc  string
	URL   string
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
	numStep := 1

	r.Session.AddUserMessage(task)

	for {
		reply, err := r.Session.Send(ctx)
		if err != nil {
			return err
		}

		if reply == "" {
			r.vlog("Empty reply - raw:\n%s", reply)
			continue
		}

		cmd, err := decodeCommand(reply)
		if err != nil {
			return err
		}

		if cmd.Thought == "" {
			r.vlog("No thought - raw:\n%s", reply)
			continue
		}

		fmt.Printf("Step %d: %s\n", numStep, cmd.Thought)
		fmt.Printf("Action: %s\n", cmd.Action)
		if cmd.Input != "" {
			fmt.Printf("%s\n", cmd.Input)
		}

		if cmd.Action == "done" {
			r.Session.AddAssistantMessage(encodeCommand(cmd))
			done = true
			break
		}

		feedback, err := r.runCommand(cmd)
		if err != nil {
			if errors.Is(err, errInvalidAction) {
				r.vlog("Invalid action - raw:\n%s", reply)
				continue
			}
			return fmt.Errorf("failed to run command: %v", err)
		}
		fmt.Printf("%s\n\n", feedback.Feedback)

		r.Session.AddAssistantMessage(encodeCommand(cmd))
		r.Session.AddUserMessage(encodeCommand(feedback))

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

func (r *Runner) runCommand(cmd *Command) (*Command, error) {
	var (
		feedback string
		err      error
	)

	switch cmd.Action {
	case "file":
		feedback, err = r.runFileCommand(cmd)
	case "cd":
		feedback, err = r.runChangeDirCommand(cmd)
	case "python":
		feedback, err = r.runPythonCommand(cmd)
	case "shell":
		feedback, err = r.runShellCommand(cmd)
	case "search":
		feedback, err = r.runSearchCommand(cmd)
	default:
		err = errInvalidAction
	}

	if err != nil {
		return nil, err
	}

	return &Command{
		Feedback: strings.TrimRight(feedback, "\n"),
	}, nil
}

func (r *Runner) runFileCommand(cmd *Command) (string, error) {
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

func (r *Runner) runPythonCommand(cmd *Command) (string, error) {
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

	outputStr := string(output)
	if len(output) == 0 {
		outputStr = fmt.Sprintf("%s (no output)", FeedbackSuccess)
	}

	return outputStr, nil
}

func (r *Runner) runShellCommand(cmd *Command) (string, error) {
	var (
		output []byte
		err    error
	)

	workDir := r.getWorkDir()

	shell := exec.Command("bash", "-e", "-o", "pipefail", "-c", cmd.Input)
	shell.Dir = workDir

	output, err = shell.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(exitErr.Stderr), nil
		}
		return "", err
	}

	err = os.WriteFile(filepath.Join(workDir, "output.log"), output, 0644)
	if err != nil {
		return "", err
	}

	if len(output) == 0 {
		return fmt.Sprintf("%s (no output)", FeedbackSuccess), nil
	}

	outputStr := string(output)

	lines := strings.Split(outputStr, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
		outputStr = strings.Join(lines, "\n")
	}

	return outputStr, nil
}

func (r *Runner) runSearchCommand(cmd *Command) (string, error) {
	var input SearchCommandInput

	err := yaml.Unmarshal([]byte(cmd.Input), &input)
	if err != nil {
		return "", err
	}

	if input.Query == "" {
		return "query must be specified", nil
	}

	payload := url.Values{}
	payload.Add("q", input.Query)
	payload.Add("kl", "")
	payload.Add("df", "")

	req, err := http.NewRequest("POST", "https://lite.duckduckgo.com/lite/", strings.NewReader(payload.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://lite.duckduckgo.com")
	req.Header.Set("User-Agent", "gptask")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("failed to get search result (status: %d)", res.StatusCode)
	}

	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", err
	}

	results := []SearchResult{}
	doc.Find("body > form > div > table:nth-child(7) > tbody > tr > td").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) == 0 {
			return
		}

		index := i / 8
		if len(results) <= index {
			results = append(results, SearchResult{})
		}

		switch {
		case i%8 == 1:
			results[index].Title = text
			results[index].URL, _ = s.Find("a").Attr("href")
		case i%8 == 3:
			results[index].Desc = text
		}
	})

	if len(results) > 3 {
		results = results[:3]
	}

	lines := []string{}
	for i := range results {
		lines = append(lines, fmt.Sprintf("%d. %s: %s", i+1, results[i].Title, results[i].Desc))
	}

	return strings.Join(lines, "\n"), nil
}

func (r *Runner) runChangeDirCommand(cmd *Command) (string, error) {
	var input ChangeDirCommandInput

	err := yaml.Unmarshal([]byte(cmd.Input), &input)
	if err != nil {
		return "", err
	}

	if input.Dir == "" {
		return "directory path must be specified", nil
	}

	if !filepath.IsAbs(input.Dir) {
		input.Dir = filepath.Join(r.getWorkDir(), input.Dir)
	}
	r.WorkDir = filepath.Clean(input.Dir)

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

func decodeCommand(reply string) (*Command, error) {
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

	return &cmd, nil
}

func encodeCommand(cmd *Command) string {
	var lines []string

	switch {
	case cmd.Thought != "":
		lines = append(lines, fmt.Sprintf("thought: %s", cmd.Thought))
		lines = append(lines, fmt.Sprintf("action: %s", cmd.Action))
		lines = append(lines, fmt.Sprintf("input:\n```\n%s\n```\n", strings.Trim(cmd.Input, "\n")))
	case cmd.Feedback != "":
		lines = append(lines, fmt.Sprintf("feedback:\n```\n%s\n```\n", strings.Trim(cmd.Feedback, "\n")))
	}

	return strings.Join(lines, "\n")
}
