package main

// --logs: todo el log y una traza de movimiento tick a tick en un archivo por
// sesión, logs/sesion-AAAAMMDD-HHMMSS.log, para revisarlo después.
//
//	go run . --logs

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var logsFlag = flag.Bool("logs", false, "guardar el log y una traza de movimiento por tick en logs/sesion-*.log")

// session es el archivo de la sesión (nil sin --logs).
var session *sessionLog

type sessionLog struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	path string
	stop chan struct{}
}

// openSession crea logs/sesion-<fecha>.log y lo vacía a disco cada segundo.
func openSession() (*sessionLog, error) {
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join("logs", "sesion-"+time.Now().Format("20060102-150405")+".log")
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	s := &sessionLog{f: f, w: bufio.NewWriterSize(f, 64<<10), path: path, stop: make(chan struct{})}
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.mu.Lock()
				_ = s.w.Flush()
				s.mu.Unlock()
			}
		}
	}()
	return s, nil
}

// Write recibe las líneas del log (log.SetOutput).
func (s *sessionLog) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(b)
}

// Trace escribe una línea solo en el archivo (no en la consola).
func (s *sessionLog) Trace(format string, args ...any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
}

func (s *sessionLog) Close() {
	if s == nil {
		return
	}
	close(s.stop)
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.w.Flush()
	_ = s.f.Close()
}

// sessionWriter es dónde más escribir el log: el archivo de la sesión, o nada.
func sessionWriter() io.Writer {
	if session == nil {
		return io.Discard
	}
	return session
}

// problemsFile es el índice de los problemas de todas las sesiones: una línea
// por problema (cuándo, en qué sesión, su número y qué pasó). La ficha entera
// ("PROBLEMA #n") está en el log de esa sesión.
const problemsFile = "logs/problemas.log"

var problemsMu sync.Mutex

// noteProblem apunta un problema en el índice (solo con --logs: sin el log de
// la sesión no hay ficha a la que apuntar).
func noteProblem(n int, task, msg string) {
	if session == nil {
		return
	}
	problemsMu.Lock()
	defer problemsMu.Unlock()
	f, err := os.OpenFile(problemsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s PROBLEMA #%d [%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), filepath.Base(session.path), n, task, msg)
}
