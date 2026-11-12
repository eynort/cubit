package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAIOpenAI: con --ai-api openai, la conversación (con una llamada a una
// herramienta y su respuesta) va en el formato de OpenAI y la respuesta
// vuelve como la de Ollama.
func TestAIOpenAI(t *testing.T) {
	var got struct {
		Model    string   `json:"model"`
		Messages []oaiMsg `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "mal", 400)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"x","type":"function","function":{"name":"run_command","arguments":"{\"command\":\"/stop\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
	}))
	defer srv.Close()
	old := *aiAPIFlag
	*aiAPIFlag = "openai"
	defer func() { *aiAPIFlag = old }()
	t.Setenv("MCBOT_AI_KEY", "k")

	a := newAI(srv.URL+"/v1", "m", 4096)
	msgs := []aiMsg{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "para"},
		{Role: "assistant", ToolCalls: []aiToolCall{{Function: aiFunc{Name: "run_command", Arguments: map[string]any{"command": "/stop"}}}}},
		{Role: "tool", ToolName: "run_command", Content: "parado"},
	}
	r, err := a.chat(context.Background(), msgs, aiTools, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Function.Arguments["command"] != "/stop" || r.PromptEval != 12 {
		t.Errorf("respuesta: %+v", r)
	}
	if got.Model != "m" || len(got.Messages) != 4 {
		t.Fatalf("petición: %+v", got)
	}
	as, tool := got.Messages[2], got.Messages[3]
	if len(as.ToolCalls) != 1 || as.ToolCalls[0].Function.Arguments != `{"command":"/stop"}` || tool.ToolCallID != as.ToolCalls[0].ID {
		t.Errorf("herramienta mal enlazada: %+v / %+v", as, tool)
	}
}
