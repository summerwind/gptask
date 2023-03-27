package main

import (
	"context"
	"strings"

	"github.com/sashabaranov/go-openai"
)

type Session struct {
	Client   *openai.Client
	Model    string
	Messages []openai.ChatCompletionMessage
}

func NewSession(c *Config) *Session {
	messages := []openai.ChatCompletionMessage{
		{Role: "system", Content: systemContent},
	}

	return &Session{
		Client:   openai.NewClient(c.APIKey),
		Model:    c.Model,
		Messages: messages,
	}
}

func (s *Session) AddUserMessage(msg string) {
	s.Messages = append(s.Messages, openai.ChatCompletionMessage{
		Role:    "user",
		Content: msg,
	})
}

func (s *Session) AddAssistantMessage(msg string) {
	s.Messages = append(s.Messages, openai.ChatCompletionMessage{
		Role:    "assistant",
		Content: msg,
	})
}

func (s *Session) Send(ctx context.Context) (string, error) {
	req := openai.ChatCompletionRequest{
		Model:       s.Model,
		Messages:    s.Messages,
		Temperature: 0.0,
		Stop:        []string{"feedback:"},
	}

	res, err := s.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	reply := res.Choices[0].Message.Content
	reply = strings.TrimRight(reply, "\n")

	return reply, nil
}
