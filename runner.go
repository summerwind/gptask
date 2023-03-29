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

type FileActionInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ChangeDirActionInput struct {
	Dir string `json:"dir"`
}

type SearchActionInput struct {
	Query string `json:"query"`
}

type SearchActionQueryResult struct {
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

		s, err := decodeStep(reply)
		if err != nil {
			return err
		}

		if s.Thought == "" {
			r.vlog("No thought - raw:\n%s", reply)
			continue
		}

		fmt.Printf("Step %d: %s\n", numStep, s.Thought)
		fmt.Printf("Action: %s\n", s.Action)
		if s.Input != "" {
			fmt.Printf("%s\n", s.Input)
		}

		if s.Action == "done" {
			done = true
			break
		}

		feedback, err := r.runAction(s)
		if err != nil {
			if errors.Is(err, errInvalidAction) {
				r.vlog("Invalid action - raw:\n%s", reply)
				continue
			}
			return fmt.Errorf("failed to run command: %v", err)
		}
		fmt.Printf("%s\n\n", feedback.Feedback)

		r.Session.AddAssistantMessage(encodeStep(s))
		r.Session.AddUserMessage(encodeStep(feedback))

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

func (r *Runner) runAction(s *Step) (*Step, error) {
	var (
		feedback string
		err      error
	)

	switch s.Action {
	case "file":
		feedback, err = r.runFileAction(s)
	case "cd":
		feedback, err = r.runChangeDirAction(s)
	case "python":
		feedback, err = r.runPythonAction(s)
	case "shell":
		feedback, err = r.runShellAction(s)
	case "search":
		feedback, err = r.runSearchAction(s)
	default:
		err = errInvalidAction
	}

	if err != nil {
		return nil, err
	}

	return &Step{
		Feedback: strings.TrimRight(feedback, "\n"),
	}, nil
}

func (r *Runner) runFileAction(s *Step) (string, error) {
	var input FileActionInput

	err := yaml.Unmarshal([]byte(s.Input), &input)
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

func (r *Runner) runPythonAction(s *Step) (string, error) {
	python := exec.Command("python3", "-c", s.Input)
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

func (r *Runner) runShellAction(s *Step) (string, error) {
	var (
		output []byte
		err    error
	)

	workDir := r.getWorkDir()

	shell := exec.Command("bash", "-e", "-o", "pipefail", "-c", s.Input)
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

func (r *Runner) runSearchAction(s *Step) (string, error) {
	var input SearchActionInput

	err := yaml.Unmarshal([]byte(s.Input), &input)
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

	results := []SearchActionQueryResult{}
	doc.Find("body > form > div > table:nth-child(7) > tbody > tr > td").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) == 0 {
			return
		}

		index := i / 8
		if len(results) <= index {
			results = append(results, SearchActionQueryResult{})
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

func (r *Runner) runChangeDirAction(s *Step) (string, error) {
	var input ChangeDirActionInput

	err := yaml.Unmarshal([]byte(s.Input), &input)
	if err != nil {
		return "", err
	}

	if input.Dir == "" {
		return "directory path must be specified", nil
	}

	if !filepath.IsAbs(input.Dir) {
		input.Dir = filepath.Join(r.getWorkDir(), input.Dir)
	}
	input.Dir = filepath.Clean(input.Dir)

	err = os.MkdirAll(input.Dir, 0755)
	if err != nil {
		return err.Error(), nil
	}

	r.WorkDir = input.Dir

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
