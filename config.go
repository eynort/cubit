package main

// A qué servidor y con qué cuenta entra el bot: nada fijo en el código.
//
//	minecraft-bot mc.ejemplo.com                 (puerto por SRV o 25565)
//	minecraft-bot --name Otro_Bot 1.2.3.4:25570
//	minecraft-bot                                (el último servidor y nombre)
//
// Lo que el bot aprende del mundo (almacén, caminos, la obra, las notas para
// la IA) va en datos/<servidor>/: cada servidor, lo suyo.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	serverFlag   = flag.String("server", "", "servidor: host o host:puerto (sin puerto, el del registro SRV o 25565); también vale como primer argumento. Vacío = el último")
	nameFlag     = flag.String("name", "", "nombre del bot (3-16 letras, números o _). Vacío = el último usado")
	ownerFlag    = flag.String("owner", "", "jugadores que pueden mandarle órdenes por /msg, separados por comas (* = cualquiera; se guarda en bot.json)")
	configFlag   = flag.String("config", "bot.json", "archivo con el último servidor, los dueños y las cuentas de cada servidor")
	defaultsFlag = flag.Bool("defaults", false, "los flags de configuración (servidor, nombre, dueños) solo valen la primera vez: después manda bot.json, lo que cambies en el panel (para Docker)")
)

// Lo que sale de la configuración (loadConfig, antes de conectar).
var (
	serverAddr string // host o host:puerto, tal como se conecta
	serverHost string // solo el host (para la IA y la carpeta de datos)
	botName    string
	dataDir    = "." // datos/<servidor>
)

// remoteOwners son los jugadores que pueden mandarle órdenes al bot por /msg
// (como si las escribieran en su consola). "*" = cualquiera: cuidado, entonces
// cualquier jugador puede hacerle pelear, escribir en el chat o desconectarlo.
var remoteOwners []string

// botConfig es bot.json.
type botConfig struct {
	Server     string   `json:"server"` // el último
	Name       string   `json:"name"`   // el último
	Owners     []string `json:"owners"`
	PanelToken string   `json:"panel_token,omitempty"` // la clave del panel web (panel.go)
	Seeded     bool     `json:"seeded,omitempty"`      // ya se usaron los flags una vez (--defaults)
}

var (
	cfgMu   sync.Mutex
	cfg     botConfig
	cfgPath string
)

// hostOf quita el puerto.
func hostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(addr, "[]")
}

// loadConfig lee bot.json y los flags, y deja listos serverAddr, botName,
// remoteOwners y dataDir. Guarda lo nuevo.
func loadConfig() error {
	cfgPath = *configFlag
	if b, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("%s: %w", cfgPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// keep: los flags ya se usaron (--defaults) o el panel reinició el bot:
	// manda bot.json.
	keep := ((*defaultsFlag && cfg.Seeded) || os.Getenv(keepConfigEnv) != "") && !*demoFlag
	cfg.Seeded = cfg.Seeded || *defaultsFlag
	if keep {
		*serverFlag, *nameFlag, *ownerFlag = "", "", ""
	}

	serverAddr = *serverFlag
	if serverAddr == "" && flag.NArg() > 0 && !(keep && cfg.Server != "") {
		serverAddr = flag.Arg(0)
	}
	if serverAddr == "" {
		serverAddr = cfg.Server
	}
	if serverAddr == "" {
		msg := "¿a qué servidor? minecraft-bot [--name <nombre>] [--owner <jugador>] <host[:puerto]>; la próxima vez sin nada entra al último"
		return errors.New(msg)
	}
	serverHost = hostOf(serverAddr)

	botName = *nameFlag
	if botName == "" {
		botName = cfg.Name
	}
	if botName == "" {
		return errors.New("¿con qué nombre? --name <nombre> (se recuerda para la próxima)")
	}
	if !validName.MatchString(botName) {
		return fmt.Errorf("nombre %q inválido: deben ser 3-16 caracteres (letras, números o _), tiene %d", botName, len(botName))
	}

	if *ownerFlag != "" {
		cfg.Owners = nil
		for _, o := range strings.Split(*ownerFlag, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.Owners = append(cfg.Owners, o)
			}
		}
	}
	remoteOwners = cfg.Owners
	if len(remoteOwners) == 0 {
		log.Print("sin dueños: nadie puede darle órdenes por /msg (--owner <jugador>)")
	}

	cfg.Server, cfg.Name = serverAddr, botName
	if err := saveConfigLocked(); err != nil {
		log.Printf("%s: no se guardó: %v", cfgPath, err)
	}

	dataDir = filepath.Join("datos", safeDirName(serverHost))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	return nil
}

func saveConfigLocked() error {
	if cfgPath == "" {
		return nil
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil { // (lleva contraseñas)
		return err
	}
	return os.Rename(tmp, cfgPath)
}

// dataFile es la ruta de un archivo de datos de este servidor.
func dataFile(name string) string { return filepath.Join(dataDir, name) }

// safeDirName: el host como nombre de carpeta.
func safeDirName(host string) string {
	host = strings.ToLower(host)
	var sb strings.Builder
	for _, r := range host {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	if s := strings.Trim(sb.String(), "."); s != "" {
		return s
	}
	return "servidor"
}
