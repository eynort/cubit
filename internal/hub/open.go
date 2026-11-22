package hub

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// openBrowser abre una dirección en el navegador del sistema (sin esperar).
func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err == nil {
		go func() { _ = c.Wait() }()
	}
}

// localURL: la dirección del panel para abrirla desde esta máquina.
func localURL(addr, token string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "8080"
	}
	return "http://localhost:" + port + "/?t=" + token
}

// openRunning: si ya hay un hub escuchando en addr, abre el navegador en él y
// dice que sí (con el token de hub.json de la carpeta de datos actual).
func openRunning(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	var cfg hubConfig
	if b, err := os.ReadFile(hubFile); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	openBrowser(localURL(addr, cfg.Token))
	return true
}
