package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"gopkg.in/yaml.v3"

	"github.com/summerwind/gptask/config"
	"github.com/summerwind/gptask/log"
	"github.com/summerwind/gptask/session"
	"github.com/summerwind/gptask/shell"
)

//go:embed prompt.txt
var systemPrompt string

var (
	errInvalidFormat = errors.New("invalid format")
	errInvalidAction = errors.New("invalid action")
)

const (
	ObservationSuccess             = "success"
	ObservationSuccessWithNoOutput = "success (no output)"
)

type FileActionInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
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
	config  *config.Config
	session *session.Session
	shell   *shell.Shell
}

func NewRunner(c *config.Config) *Runner {
	return &Runner{
		config:  c,
		session: session.New(c, systemPrompt),
		shell:   shell.New(),
	}
}

func (r *Runner) Run(task string) error {
	var done bool

	ctx := context.Background()
	numStep := 1

	err := r.shell.Start()
	if err != nil {
		return err
	}
	defer r.shell.Stop()

	r.session.AddUserMessage(task)

	for {
		var (
			obs string
			err error
		)

		reply, err := r.session.GetCompletion(ctx)
		if err != nil {
			return err
		}
		log.Debug("reply", reply)

		if reply == "" {
			log.Debug("retry", "empty reply")
			continue
		}

		s, err := decodeStep(reply)
		if err != nil {
			log.Debug("retry", err.Error())
			continue
		}

		log.Comment(fmt.Sprintf("Step %d: %s", numStep, s.Thought))

		if s.Action == "done" {
			done = true
			break
		}

		switch s.Action {
		case "file":
			obs, err = r.runFileAction(s)
		case "python":
			obs, err = r.runPythonAction(s)
		case "shell":
			obs, err = r.runShellAction(s)
		case "search":
			obs, err = r.runSearchAction(s)
		default:
			err = errInvalidAction
		}

		if err != nil {
			if errors.Is(err, errInvalidAction) {
				log.Debug("retry", err.Error())
				continue
			}
			return fmt.Errorf("failed to run command: %v", err)
		}

		log.Stdout("")

		r.session.AddAssistantMessage(encodeStep(s))
		r.session.AddUserMessage(encodeStep(&Step{Observation: obs}))

		numStep += 1
		if numStep > r.config.MaxSteps {
			break
		}
	}

	if done {
		log.Comment("Done")
	} else {
		log.Comment("The maximum number of steps has been reached")
	}

	return nil
}

func (r *Runner) runFileAction(s *Step) (string, error) {
	var (
		input  FileActionInput
		output string
	)

	err := yaml.Unmarshal([]byte(s.Input), &input)
	if err != nil {
		return "", err
	}

	log.Command(fmt.Sprintf("vim %s", input.Path))

	if input.Path == "" {
		output = "file path must be specified"
		log.Stderr(output)
		return output, nil
	}

	if !filepath.IsAbs(input.Path) {
		input.Path = filepath.Join(r.shell.WorkDir(), input.Path)
	}

	dirPath := filepath.Dir(input.Path)
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(input.Path, []byte(input.Content), 0644)
	if err != nil {
		return "", err
	}

	log.CodeBlock(input.Content)

	return ObservationSuccess, nil
}

func (r *Runner) runPythonAction(s *Step) (string, error) {
	log.Command("python3")
	log.CodeBlock(s.Input)

	python := exec.Command("python3", "-c", s.Input)
	python.Dir = r.shell.WorkDir()

	output, err := python.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Stderr(string(output))
			return string(output), nil

		}
		return "", err
	}

	outputStr := strings.TrimRight(string(output), "\n")
	if outputStr == "" {
		outputStr = ObservationSuccessWithNoOutput
	}

	log.Stdout(outputStr)

	return outputStr, nil
}

func (r *Runner) runShellAction(s *Step) (string, error) {
	var (
		rc     int
		stdout string
		stderr string
		err    error
	)

	commands := strings.Split(s.Input, "\n")
	for _, cmd := range commands {
		log.Command(cmd)

		rc, stdout, stderr, err = r.shell.Run(cmd)
		if err != nil {
			return "", err
		}

		if rc != 0 {
			if len(stderr) == 0 {
				return fmt.Sprintf("failed (exit code: %d)", rc), nil
			}
			return stderr, nil
		}
	}

	if len(stdout) == 0 {
		return ObservationSuccessWithNoOutput, nil
	}

	return stdout, nil
}

func (r *Runner) runSearchAction(s *Step) (string, error) {
	var output string

	log.Command(fmt.Sprintf("search %s", s.Input))

	if s.Input == "" {
		output = "query must be specified"
		log.Stderr(output)
		return output, nil
	}

	payload := url.Values{}
	payload.Add("q", s.Input)
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
	output = strings.Join(lines, "\n")

	log.CodeBlock(output)

	return output, nil
}
