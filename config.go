package main

// A qué servidor y con qué cuenta entra el bot: nada fijo en el código.
//
//	minecraft-bot mc.ejemplo.com                 (puerto por SRV o 25565)
//	minecraft-bot --name Otro_Bot 1.2.3.4:25570
//	minecraft-bot                                (el último servidor y nombre)
//
// Las contraseñas del plugin de login (AuthMe, nLogin, LibreLogin, VeloAuth…)
// se guardan por servidor y nombre en bot.json; si un servidor pide
// registrarse y no hay ninguna, el bot se inventa una, se registra y la
// guarda. Lo que el bot aprende del mundo (almacén, caminos, la obra, las
// notas para la IA) va en datos/<servidor>/: cada servidor, lo suyo.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	serverFlag   = flag.String("server", "", "servidor: host o host:puerto (sin puerto, el del registro SRV o 25565); también vale como primer argumento. Vacío = el último")
	nameFlag     = flag.String("name", "", "nombre del bot (3-16 letras, números o _). Vacío = el último usado")
	passwordFlag = flag.String("password", "", "contraseña del plugin de login de ese servidor (se guarda en bot.json). Vacío = la guardada, o una nueva al registrarse; también MCBOT_PASSWORD")
	ownerFlag    = flag.String("owner", "", "jugadores que pueden mandarle órdenes por /msg, separados por comas (* = cualquiera; se guarda en bot.json)")
	skinFlag     = flag.String("skin", "", `skin para este servidor con /skin de SkinsRestorer (ej: 'url "https://…" classic'; "-" = ninguna; se guarda)`)
	configFlag   = flag.String("config", "bot.json", "archivo con el último servidor, los dueños y las cuentas de cada servidor")
)

// Lo que sale de la configuración (loadConfig, antes de conectar).
var (
	serverAddr  string // host o host:puerto, tal como se conecta
	serverHost  string // solo el host (para la IA y la carpeta de datos)
	botName     string
	botPassword string // "" = no se sabe (al registrarse se inventa una)
	botSkin     string // "" = la de siempre
	dataDir     = "."  // datos/<servidor>
)

// remoteOwners son los jugadores que pueden mandarle órdenes al bot por /msg
// (como si las escribieran en su consola). "*" = cualquiera: cuidado, entonces
// cualquier jugador puede hacerle pelear, escribir en el chat o desconectarlo.
var remoteOwners []string

// botConfig es bot.json.
type botConfig struct {
	Server     string              `json:"server"` // el último
	Name       string              `json:"name"`   // el último
	Owners     []string            `json:"owners"`
	Webhook    string              `json:"webhook,omitempty"`     // avisos (notify.go)
	PanelToken string              `json:"panel_token,omitempty"` // la clave del panel web (panel.go)
	Limits     limits              `json:"limits"`                // lo que no hace nunca (limits.go)
	Accounts   map[string]*account `json:"accounts"`              // clave: accountKey
}

type account struct {
	Password string `json:"password,omitempty"`
	Skin     string `json:"skin,omitempty"`
}

var (
	cfgMu   sync.Mutex
	cfg     botConfig
	cfgPath string
)

// accountKey: "host/nombre" (en minúsculas, sin puerto: el mismo servidor por
// SRV o con el puerto puesto es la misma cuenta).
func accountKey(host, name string) string {
	return strings.ToLower(host) + "/" + strings.ToLower(name)
}

// hostOf quita el puerto.
func hostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(addr, "[]")
}

// loadConfig lee bot.json y los flags, y deja listos serverAddr, botName,
// botPassword, botSkin, remoteOwners y dataDir. Guarda lo nuevo.
func loadConfig() error {
	cfgPath = *configFlag
	if b, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("%s: %w", cfgPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if cfg.Accounts == nil {
		cfg.Accounts = map[string]*account{}
	}

	serverAddr = *serverFlag
	if serverAddr == "" && flag.NArg() > 0 {
		serverAddr = flag.Arg(0)
	}
	if serverAddr == "" {
		serverAddr = cfg.Server
	}
	if serverAddr == "" {
		msg := "¿a qué servidor? minecraft-bot [--name <nombre>] [--owner <jugador>] <host[:puerto]>; la próxima vez sin nada entra al último"
		if ks := knownServers(); len(ks) > 0 {
			msg += " (con cuenta: " + strings.Join(ks, ", ") + ")"
		}
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
	if len(remoteOwners) == 0 {
		log.Print("sin dueños: nadie puede darle órdenes por /msg (--owner <jugador>)")
	}

	acc := cfg.Accounts[accountKey(serverHost, botName)]
	if acc == nil {
		acc = &account{}
		cfg.Accounts[accountKey(serverHost, botName)] = acc
	}
	if p := *passwordFlag; p == "-" {
		acc.Password = "" // (ninguna: se inventa una si hace falta)
	} else if p != "" {
		acc.Password = p
	} else if p := os.Getenv("MCBOT_PASSWORD"); p != "" {
		acc.Password = p
	}
	switch *skinFlag {
	case "":
	case "-":
		acc.Skin = ""
	default:
		acc.Skin = *skinFlag
	}
	botPassword, botSkin = acc.Password, acc.Skin

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

// rememberPassword guarda la contraseña de esta cuenta (la que se inventó al
// registrarse, o la que se escribió a mano en la consola).
func rememberPassword(p string) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	botPassword = p
	if cfg.Accounts == nil {
		cfg.Accounts = map[string]*account{}
	}
	k := accountKey(serverHost, botName)
	if cfg.Accounts[k] == nil {
		cfg.Accounts[k] = &account{}
	}
	if cfg.Accounts[k].Password == p {
		return
	}
	cfg.Accounts[k].Password = p
	if err := saveConfigLocked(); err != nil {
		log.Printf("%s: no se guardó la contraseña: %v", cfgPath, err)
	}
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

// newPassword: una contraseña para registrarse que pasan las reglas de los
// plugins de login (larga, con mayúsculas, minúsculas y números, sin
// símbolos que alguno rechaza).
func newPassword() string {
	const lower, upper, digits = "abcdefghijkmnpqrstuvwxyz", "ABCDEFGHJKLMNPQRSTUVWXYZ", "23456789"
	pick := func(set string) byte {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		return set[n.Int64()]
	}
	b := []byte{pick(upper)}
	for i := 0; i < 9; i++ {
		b = append(b, pick(lower+upper+digits))
	}
	b = append(b, pick(lower), pick(digits), pick(digits))
	return string(b)
}

// knownServers: los servidores con cuenta guardada (para el aviso de uso).
func knownServers() []string {
	seen := map[string]bool{}
	var out []string
	for k := range cfg.Accounts {
		h := k[:strings.LastIndex(k, "/")]
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}
