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
	chestsFlag   = flag.String("chests", "", "on: guarda en cofres (el de minar, el almacén, los de alrededor; por defecto); off: no usa cofres, trabaja con lo que le quepa encima (se guarda)")
	defaultsFlag = flag.Bool("defaults", false, "los flags de configuración (servidor, nombre, dueños, webhook, límites, IA) solo valen la primera vez: después manda bot.json, lo que cambies en el panel (para Docker)")
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
	Server     string      `json:"server"` // el último
	Name       string      `json:"name"`   // el último
	Owners     []string    `json:"owners"`
	Webhook    string      `json:"webhook,omitempty"`     // avisos (notify.go)
	PanelToken string      `json:"panel_token,omitempty"` // la clave del panel web (panel.go)
	Limits     limits      `json:"limits"`                // lo que no hace nunca (limits.go)
	NoChests   bool        `json:"no_chests,omitempty"`   // no usar cofres (/chests off)
	AI         *aiSettings `json:"ai,omitempty"`          // la IA (la última; se cambia en el panel)
	Seeded     bool        `json:"seeded,omitempty"`      // ya se usaron los flags una vez (--defaults)
}

// aiSettings: --ai, --ai-api y --ai-model, recordados.
type aiSettings struct {
	URL   string `json:"url"`
	API   string `json:"api"`
	Model string `json:"model"`
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
		*serverFlag, *nameFlag, *ownerFlag, *webhookFlag = "", "", "", ""
		*areaFlag, *pvpFlag, *neverBreakFlag, *chestsFlag = "", "", "", ""
	}
	applyAISettings(keep)

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
	applyWebhookFlag()
	applyLimitFlags()
	switch strings.ToLower(*chestsFlag) {
	case "on", "true", "si", "sí", "yes":
		cfg.NoChests = false
	case "off", "false", "no":
		cfg.NoChests = true
	case "":
	default:
		return fmt.Errorf("--chests: on u off, no %q", *chestsFlag)
	}
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

// applyAISettings: --ai, --ai-api y --ai-model se recuerdan en bot.json; lo
// guardado vale si no se ponen a mano (o siempre, con keep).
func applyAISettings(keep bool) {
	set := map[string]bool{}
	if !keep {
		flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	}
	if a := cfg.AI; a != nil {
		if !set["ai"] {
			*aiURLFlag = a.URL
		}
		if !set["ai-api"] && a.API != "" {
			*aiAPIFlag = a.API
		}
		if !set["ai-model"] && a.Model != "" {
			*aiModelFlag = a.Model
		}
	}
	cfg.AI = &aiSettings{URL: *aiURLFlag, API: *aiAPIFlag, Model: *aiModelFlag}
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

// chestsOn: si el bot usa cofres (bot.json).
func chestsOn() bool {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return !cfg.NoChests
}

// setChests guarda si usa cofres y lo aplica al bot conectado.
func setChests(on bool) error {
	cfgMu.Lock()
	cfg.NoChests = !on
	err := saveConfigLocked()
	cfgMu.Unlock()
	if b := current.Load(); b != nil && b.Tasks != nil {
		b.Tasks.SetChests(on)
	}
	return err
}

// cmdChests: /chests [on|off].
func cmdChests(_ *Bot, args []string) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "on":
			if err := setChests(true); err != nil {
				return err
			}
		case "off":
			if err := setChests(false); err != nil {
				return err
			}
		default:
			return errf("/chests on|off", "/chests on|off")
		}
	}
	if chestsOn() {
		log.Print(tr("chests: on (it stores what it gathers; if it finds no chest it says so and keeps working)", "cofres: sí (guarda lo que saca; si no encuentra cofre, lo dice y sigue trabajando)"))
	} else {
		log.Print(tr("chests: off (it never uses chests: it keeps what fits, the rest stays on the ground)", "cofres: no (no usa cofres: se queda lo que le quepa, lo demás se queda en el suelo)"))
	}
	return nil
}
