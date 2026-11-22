package main

// Ollama: el modelo local (HTTP, solo la librería estándar). La dirección sale
// del panel, de --ollama, de OLLAMA_URL (en Docker: http://ollama:11434) o, sin
// nada, http://localhost:11434. El modelo, del panel, de OLLAMA_MODEL o gemma4:e2b-it-q4_K_M.
// Si el modelo no está bajado, el bot lo baja solo (POST /api/pull) y el panel
// enseña el progreso.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var ollamaFlag = flag.String("ollama", "", "dirección de Ollama para la IA (ej: http://192.168.1.5:11434); vacío = OLLAMA_URL o http://localhost:11434")

const (
	// Gemma 4 e2b cuantizado a q4 (4,6 GB de descarga, ~3-4 GB de RAM libre).
	// Alternativas: gemma4:e4b (más lista, más RAM), qwen3:4b.
	llmDefaultModel = "gemma4:e2b-it-q4_K_M"
	llmDefaultURL   = "http://localhost:11434"
)

// llmURL y llmModelName: la configuración que manda ahora.
func llmURL() string {
	cfgMu.Lock()
	u := strings.TrimSpace(cfg.LLM.URL)
	cfgMu.Unlock()
	for _, c := range []string{u, *ollamaFlag, os.Getenv("OLLAMA_URL"), llmDefaultURL} {
		if c = strings.TrimSpace(c); c != "" {
			return strings.TrimRight(c, "/")
		}
	}
	return llmDefaultURL
}

func llmModelName() string {
	cfgMu.Lock()
	m := strings.TrimSpace(cfg.LLM.Model)
	cfgMu.Unlock()
	for _, c := range []string{m, os.Getenv("OLLAMA_MODEL"), llmDefaultModel} {
		if c = strings.TrimSpace(c); c != "" {
			return c
		}
	}
	return llmDefaultModel
}

type ollamaModel struct {
	url, model string
	hc         *http.Client
}

func newOllamaModel() llmModel {
	return &ollamaModel{url: llmURL(), model: llmModelName(), hc: &http.Client{Timeout: 4 * time.Minute}}
}

// Chat manda la conversación con la respuesta forzada al esquema JSON.
func (m *ollamaModel) Chat(ctx context.Context, system string, msgs []llmMsg) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	ms := []message{{"system", system}}
	for _, x := range msgs {
		ms = append(ms, message{x.Role, x.Text})
	}
	body, _ := json.Marshal(map[string]any{
		"model":      m.model,
		"messages":   ms,
		"stream":     false,
		"format":     llmSchema(),
		"keep_alive": "30m",
		"options":    map[string]any{"temperature": 0.3, "num_ctx": 4096, "num_predict": 500},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", &llmError{"other", err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", &llmError{"other", ctx.Err()}
		}
		return "", &llmError{"down", err}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		kind := "other"
		if resp.StatusCode == http.StatusNotFound {
			kind = "missing"
		}
		return "", &llmError{kind, fmt.Errorf("ollama %s: %s", resp.Status, clip(strings.TrimSpace(string(data)), 200))}
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", &llmError{"other", err}
	}
	if out.Error != "" {
		return "", &llmError{"other", errors.New(out.Error)}
	}
	return out.Message.Content, nil
}

// ---- estado y descarga del modelo ----

var llmDL struct {
	sync.Mutex
	pulling bool
	pct     int
	probed  time.Time
	state   string
}

// llmStatus: "off", "down" (Ollama no contesta), "missing", "downloading" o "ready",
// y el % de la descarga. Con caché de unos segundos (el panel pregunta a menudo).
func llmStatus() (string, int) {
	if !llmEnabled() {
		return "off", 0
	}
	llmDL.Lock()
	if llmDL.pulling {
		defer llmDL.Unlock()
		return "downloading", llmDL.pct
	}
	if time.Since(llmDL.probed) < 5*time.Second && llmDL.state != "" {
		defer llmDL.Unlock()
		return llmDL.state, 0
	}
	llmDL.Unlock()

	state := llmProbe(llmURL(), llmModelName())
	llmDL.Lock()
	llmDL.state, llmDL.probed = state, time.Now()
	llmDL.Unlock()
	if state == "missing" {
		llmPull()
		return "downloading", 0
	}
	return state, 0
}

// llmProbe pregunta a Ollama qué modelos tiene.
func llmProbe(url, model string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/tags", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "down"
	}
	defer resp.Body.Close()
	var t struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t) != nil {
		return "down"
	}
	for _, m := range t.Models {
		if m.Name == model || m.Name == model+":latest" {
			return "ready"
		}
	}
	return "missing"
}

// llmPull baja el modelo en segundo plano (una descarga a la vez).
func llmPull() {
	llmDL.Lock()
	if llmDL.pulling {
		llmDL.Unlock()
		return
	}
	llmDL.pulling, llmDL.pct = true, 0
	llmDL.Unlock()
	url, model := llmURL(), llmModelName()
	go func() {
		err := llmPullNow(url, model)
		llmDL.Lock()
		llmDL.pulling, llmDL.probed = false, time.Time{}
		llmDL.Unlock()
		if err != nil {
			log.Printf("[ia] no pude bajar %s de %s: %v", model, url, err)
		} else {
			log.Printf("[ia] modelo %s listo", model)
		}
	}()
}

func llmPullNow(url, model string) error {
	log.Printf("[ia] bajando el modelo %s de %s (la primera vez tarda)", model, url)
	body, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, err := http.NewRequest(http.MethodPost, url+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req) // sin plazo: son varios GB
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var p struct {
			Total, Completed int64
			Error            string
		}
		if json.Unmarshal(sc.Bytes(), &p) != nil {
			continue
		}
		if p.Error != "" {
			return errors.New(p.Error)
		}
		if p.Total > 0 {
			llmDL.Lock()
			llmDL.pct = int(p.Completed * 100 / p.Total)
			llmDL.Unlock()
		}
	}
	return sc.Err()
}
