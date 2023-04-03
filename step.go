package main

import (
	"errors"
	"fmt"
	"strings"
)

type Step struct {
	Thought     string `json:"thought,omitempty"`
	Action      string `json:"action,omitempty"`
	Input       string `json:"input,omitempty"`
	Observation string `json:"observation,omitempty"`
}

func (s *Step) Validate() error {
	if s.Action == "done" {
		return nil
	}

	if s.Thought == "" {
		return errors.New("thought must be specified")
	}
	if s.Input == "" {
		return errors.New("input must be specified")
	}

	switch s.Action {
	case "file", "python", "shell", "search":
		// valid.
	default:
		return errors.New("invalid action value")
	}

	return nil
}

func decodeStep(msg string) (*Step, error) {
	var (
		s     Step
		block bool
		i     int
	)

	lines := strings.Split(msg, "\n")
	lineLen := len(lines)
	input := []string{}

	for i < lineLen {
		switch {
		case strings.HasPrefix(lines[i], "thought:"):
			parts := strings.SplitN(lines[i], ":", 2)
			s.Thought = strings.Trim(parts[1], "\n ")
		case strings.HasPrefix(lines[i], "action:"):
			parts := strings.SplitN(lines[i], ":", 2)
			s.Action = strings.Trim(parts[1], "\n ")
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
			s.Input = strings.Join(input, "\n")
		}

		i += 1
	}

	err := s.Validate()
	if err != nil {
		return nil, err
	}

	return &s, nil
}

func encodeStep(s *Step) string {
	var lines []string

	switch {
	case s.Thought != "":
		lines = append(lines, fmt.Sprintf("thought: %s", s.Thought))
		lines = append(lines, fmt.Sprintf("action: %s", s.Action))
		lines = append(lines, fmt.Sprintf("input:\n```\n%s\n```\n", strings.Trim(s.Input, "\n")))
	case s.Observation != "":
		lines = append(lines, fmt.Sprintf("observation:\n```\n%s\n```\n", strings.Trim(s.Observation, "\n")))
	}

	return strings.Join(lines, "\n")
}
