package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Client wraps the Google Gemini SDK for our CLI tools.
type Client struct {
	client *genai.Client
	ctx    context.Context
	model  *genai.GenerativeModel
}

// getEnvSetInstruction returns the platform-correct shell command to set an env variable.
func getEnvSetInstruction(varName string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("PowerShell: $env:%s = \"your-key-here\"\n   CMD:        set %s=your-key-here", varName, varName)
	}
	return fmt.Sprintf("export %s=your-key-here", varName)
}

// NewClient creates a new Gemini client. Reads GEMINI_API_KEY from env.
func NewClient() (*Client, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set.\n   Set it using:\n   %s", getEnvSetInstruction("GEMINI_API_KEY"))
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	// Use flash for CLI speed
	model := client.GenerativeModel("gemini-2.5-flash")

	// Enforce JSON outputs across all our AI tools
	model.ResponseMIMEType = "application/json"

	return &Client{
		client: client,
		ctx:    ctx,
		model:  model,
	}, nil
}

// Close closes the underlying SDK client.
func (c *Client) Close() {
	c.client.Close()
}

// Complete sends a request to Gemini using System Instructions and returns the text.
func (c *Client) Complete(systemPrompt, userMessage string) (string, error) {
	c.model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}

	resp, err := c.model.GenerateContent(c.ctx, genai.Text(userMessage))
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}

	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		return fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0]), nil
	}

	return "", fmt.Errorf("empty response from API")
}

// parseJSON is a helper used by both the client and log analyzer.
func parseJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}

// CompleteWithRetry wraps Complete with a simple retry loop for network blips.
func (c *Client) CompleteWithRetry(systemPrompt, userMessage string, maxRetries int) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := c.Complete(systemPrompt, userMessage)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return "", fmt.Errorf("API request failed after %d attempts. Last error: %w", maxRetries, lastErr)
}
