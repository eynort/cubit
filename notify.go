package main

// Avisos fuera del juego: a un webhook de Discord (o de Slack, o cualquiera
// que acepte un JSON con "content"/"text"), para enterarse sin mirar la
// consola de que murió, se atascó, terminó o le echaron.
//
//	--webhook https://discord.com/api/webhooks/…   (se guarda en bot.json)

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var webhookFlag = flag.String("webhook", "", `webhook de Discord/Slack para los avisos (muerte, atasco, tarea acabada, expulsión); se guarda en bot.json; "-" = ninguno`)

var notifier struct {
	once sync.Once
	ch   chan string
}

// notify manda un aviso (sin esperar; si se acumulan muchos, se pierden los
// de más: es un aviso, no un log).
func notify(text string) {
	url := webhookURL()
	if url == "" {
		return
	}
	notifier.once.Do(func() {
		notifier.ch = make(chan string, 16)
		go sendNotifications(notifier.ch)
	})
	select {
	case notifier.ch <- text:
	default:
	}
}

func webhookURL() string {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg.Webhook
}

func sendNotifications(ch chan string) {
	client := &http.Client{Timeout: 10 * time.Second}
	for text := range ch {
		who := botName
		if serverHost != "" {
			who += " @ " + serverHost
		}
		body, _ := json.Marshal(map[string]string{
			"username": who,
			"content":  shortChat(text, 1900), // (Discord: 2000 como mucho)
			"text":     who + ": " + text,     // (Slack)
		})
		resp, err := client.Post(webhookURL(), "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("aviso: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				time.Sleep(5 * time.Second)
			} else if resp.StatusCode >= 300 {
				log.Printf("aviso: el webhook contestó %s", resp.Status)
			}
		}
		time.Sleep(time.Second) // (sin agobiar al webhook)
	}
}

// applyWebhookFlag: --webhook a bot.json (lo llama loadConfig).
func applyWebhookFlag() {
	switch w := strings.TrimSpace(*webhookFlag); w {
	case "":
	case "-":
		cfg.Webhook = ""
	default:
		cfg.Webhook = w
	}
}
