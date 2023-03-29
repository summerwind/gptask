package main

import (
	"fmt"
	"strings"
)

type Step struct {
	Thought  string `json:"thought,omitempty"`
	Action   string `json:"action,omitempty"`
	Input    string `json:"input,omitempty"`
	Feedback string `json:"feedback,omitempty"`
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

	return &s, nil
}

func encodeStep(s *Step) string {
	var lines []string

	switch {
	case s.Thought != "":
		lines = append(lines, fmt.Sprintf("thought: %s", s.Thought))
		lines = append(lines, fmt.Sprintf("action: %s", s.Action))
		lines = append(lines, fmt.Sprintf("input:\n```\n%s\n```\n", strings.Trim(s.Input, "\n")))
	case s.Feedback != "":
		lines = append(lines, fmt.Sprintf("feedback:\n```\n%s\n```\n", strings.Trim(s.Feedback, "\n")))
	}

	return strings.Join(lines, "\n")
}
