package main

// La IA con cualquier servicio compatible con la API de OpenAI (OpenAI,
// OpenRouter, Groq, LM Studio, llama.cpp server, vLLM…), además de Ollama:
//
//	--ai-api openai --ai https://api.openai.com/v1 --ai-model gpt-4o-mini   (clave en MCBOT_AI_KEY)
//	--ai-api openai --ai http://127.0.0.1:1234/v1 --ai-model <el de LM Studio>
//
// Se traduce aquí: los mensajes y las herramientas van en el formato de
// OpenAI y la respuesta vuelve como la de Ollama (aiResp).

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

var (
	aiAPIFlag = flag.String("ai-api", "ollama", "qué API habla el servicio de --ai: ollama u openai (cualquiera compatible: OpenAI, OpenRouter, Groq, LM Studio…; la clave en MCBOT_AI_KEY)")
)

func aiKey() string {
	if k := os.Getenv("MCBOT_AI_KEY"); k != "" {
		return k
	}
	return os.Getenv("OPENAI_API_KEY")
}

type oaiMsg struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// toOpenAI: los mensajes en el formato de OpenAI. Las llamadas a
// herramientas llevan un id y cada respuesta de herramienta el de la suya
// (en Ollama van por orden).
func toOpenAI(msgs []aiMsg) []oaiMsg {
	out := make([]oaiMsg, 0, len(msgs))
	var pending []string
	for i, m := range msgs {
		o := oaiMsg{Role: m.Role, Content: m.Content}
		switch m.Role {
		case "assistant":
			pending = pending[:0]
			for j, c := range m.ToolCalls {
				var tc oaiToolCall
				tc.ID, tc.Type = fmt.Sprintf("call_%d_%d", i, j), "function"
				tc.Function.Name = c.Function.Name
				args, _ := json.Marshal(c.Function.Arguments)
				tc.Function.Arguments = string(args)
				o.ToolCalls = append(o.ToolCalls, tc)
				pending = append(pending, tc.ID)
			}
		case "tool":
			if len(pending) == 0 { // (una respuesta suelta: como texto)
				o = oaiMsg{Role: "user", Content: "[" + m.ToolName + "] " + m.Content}
				break
			}
			o.ToolCallID, pending = pending[0], pending[1:]
		}
		out = append(out, o)
	}
	return out
}

// chatOpenAI es chat() contra /chat/completions.
func (a *AI) chatOpenAI(ctx context.Context, msgs []aiMsg, tools []map[string]any, maxTokens int) (*aiResp, error) {
	body := map[string]any{"model": a.model, "messages": toOpenAI(msgs), "max_tokens": maxTokens}
	if len(tools) > 0 {
		body["tools"] = tools // (ya van en el formato de OpenAI: type function)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.url+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if k := aiKey(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content   string        `json:"content"`
				ToolCalls []oaiToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("respuesta de la IA (%s): %.200s", resp.Status, raw)
	}
	if r.Error != nil || resp.StatusCode != http.StatusOK || len(r.Choices) == 0 {
		msg := resp.Status
		if r.Error != nil {
			msg += ": " + r.Error.Message
		}
		return nil, fmt.Errorf("IA (%s)", msg)
	}
	var out aiResp
	m := r.Choices[0].Message
	out.Message.Content = m.Content
	for _, tc := range m.ToolCalls {
		var c aiToolCall
		c.Function.Name = tc.Function.Name
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &c.Function.Arguments)
		out.Message.ToolCalls = append(out.Message.ToolCalls, c)
	}
	out.PromptEval, out.Eval = r.Usage.Prompt, r.Usage.Completion
	return &out, nil
}

// checkOpenAI: ¿responde el servicio?
func (a *AI) checkOpenAI(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", a.url+"/models", nil)
	if k := aiKey(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	return nil
}
