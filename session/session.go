package session

import (
	"context"
	_ "embed"
	"strings"

	"github.com/sashabaranov/go-openai"
	"github.com/summerwind/gptask/config"
)

type Session struct {
	Client   *openai.Client
	Model    string
	Messages []openai.ChatCompletionMessage
}

func New(c *config.Config, systemPrompt string) *Session {
	return &Session{
		Client: openai.NewClient(c.APIKey),
		Model:  c.Model,
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: systemPrompt},
		},
	}
}

func (s *Session) AddUserMessage(prompt string) {
	s.Messages = append(s.Messages, openai.ChatCompletionMessage{
		Role:    "user",
		Content: prompt,
	})
}

func (s *Session) AddAssistantMessage(prompt string) {
	s.Messages = append(s.Messages, openai.ChatCompletionMessage{
		Role:    "assistant",
		Content: prompt,
	})
}

func (s *Session) GetCompletion(ctx context.Context) (string, error) {
	req := openai.ChatCompletionRequest{
		Model:       s.Model,
		Messages:    s.Messages,
		Temperature: 0.0,
		Stop:        []string{"observation:"},
	}

	res, err := s.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	reply := res.Choices[0].Message.Content
	reply = strings.TrimRight(reply, "\n")

	return reply, nil
}
