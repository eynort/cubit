// Package mcserver: el servidor de Minecraft desde el panel, cuando es tuyo
// (el local de compose.yaml): jugadores, mundo, gamerules y server.properties,
// y apagarlo, encenderlo y reiniciarlo (power.go, restart.go).
//
//	MCBOT_RCON=minecraft:25575     la consola (internal/rcon): lo que cambia al momento
//	MCBOT_SERVER_DIR=/server       su carpeta: server.properties, ops, lista blanca
//
// server.properties solo se lee al arrancar el servidor: lo que no tiene
// comando (motd, máximo de jugadores…) pide reiniciarlo (/server/restart:
// "stop", y Docker lo vuelve a arrancar).
//
// Lo que necesita saber del programa que lo usa (un bot o el hub) se lo da
// él al arrancar: DataFile, IsBotName, Bot y OnUp (hooks.go).
package mcserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cubit/internal/i18n"
	"cubit/internal/properties"
	"cubit/internal/rcon"
)

func serverDir() string { return os.Getenv("MCBOT_SERVER_DIR") }

// serverProp: una propiedad que se puede cambiar desde el panel.
type serverProp struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	LabelEn string   `json:"label_en"`
	Kind    string   `json:"kind"` // text, int, bool, choice
	Opts    []string `json:"opts,omitempty"`
	Min     int      `json:"min,omitempty"`
	Max     int      `json:"max,omitempty"`
	Live    string   `json:"-"` // el comando que lo aplica sin reiniciar (%s = el valor)
	Hint    string   `json:"hint,omitempty"`
	HintEn  string   `json:"hint_en,omitempty"`
}

var serverProps = []serverProp{
	{Key: "motd", Label: "Mensaje en la lista de servidores", LabelEn: "Message in the server list", Kind: "text"},
	{Key: "difficulty", Label: "Dificultad", LabelEn: "Difficulty", Kind: "choice", Opts: []string{"peaceful", "easy", "normal", "hard"}, Live: "difficulty %s"},
	{Key: "gamemode", Label: "Modo de juego por defecto", LabelEn: "Default game mode", Kind: "choice", Opts: []string{"survival", "creative", "adventure", "spectator"}, Live: "defaultgamemode %s"},
	{Key: "force-gamemode", Label: "Forzar el modo al entrar", LabelEn: "Force the game mode on join", Kind: "bool"},
	{Key: "hardcore", Label: "Hardcore", LabelEn: "Hardcore", Kind: "bool", Hint: "se aplica al reiniciar", HintEn: "applied on restart"},
	{Key: "max-players", Label: "Máximo de jugadores", LabelEn: "Max players", Kind: "int", Min: 1, Max: 1000},
	{Key: "view-distance", Label: "Distancia de visión (chunks)", LabelEn: "View distance (chunks)", Kind: "int", Min: 2, Max: 32},
	{Key: "simulation-distance", Label: "Distancia de simulación (chunks)", LabelEn: "Simulation distance (chunks)", Kind: "int", Min: 2, Max: 32},
	{Key: "spawn-protection", Label: "Protección del spawn (bloques)", LabelEn: "Spawn protection (blocks)", Kind: "int", Min: 0, Max: 1000},
	{Key: "allow-flight", Label: "Permitir volar (mods, élitros raros)", LabelEn: "Allow flying (mods, odd elytra)", Kind: "bool", Hint: "si no, el servidor echa a quien parece que vuela", HintEn: "otherwise the server kicks anyone who seems to fly"},
	{Key: "white-list", Label: "Lista blanca", LabelEn: "Whitelist", Kind: "bool", Live: "whitelist %s"},
	{Key: "enforce-whitelist", Label: "Echar a los que no están en la lista", LabelEn: "Kick players not on the list", Kind: "bool"},
	{Key: "pause-when-empty-seconds", Label: "Pausar si no hay nadie (segundos, 0 = no)", LabelEn: "Pause when nobody is on (seconds, 0 = no)", Kind: "int", Min: 0, Max: 86400},
	{Key: "player-idle-timeout", Label: "Echar por inactividad (minutos, 0 = no)", LabelEn: "Kick for idling (minutes, 0 = no)", Kind: "int", Min: 0, Max: 10000},
}

// gamerules: las que se ven en el panel (nombres de la 26.2).
var gamerules = []struct {
	Name, Label, Kind, LabelEn string
}{
	{"keep_inventory", "Conservar el inventario al morir", "bool", "Keep inventory on death"},
	{"pvp", "PvP entre jugadores", "bool", "PvP between players"},
	{"advance_time", "Ciclo de día y noche", "bool", "Day and night cycle"},
	{"advance_weather", "Cambios de tiempo", "bool", "Weather changes"},
	{"spawn_monsters", "Aparecen monstruos", "bool", "Monsters spawn"},
	{"mob_griefing", "Los mobs rompen bloques (creepers…)", "bool", "Mobs break blocks (creepers…)"},
	{"immediate_respawn", "Reaparecer sin pantalla de muerte", "bool", "Respawn without the death screen"},
	{"natural_health_regeneration", "Regenerar vida", "bool", "Natural health regeneration"},
	{"players_sleeping_percentage", "Jugadores durmiendo para pasar la noche (%)", "int", "Players sleeping to skip the night (%)"},
}

var validGamerule = func() map[string]string {
	m := map[string]string{}
	for _, g := range gamerules {
		m[g.Name] = g.Kind
	}
	return m
}()

// ---- server.properties ----

func serverPropsPath() string { return filepath.Join(serverDir(), "server.properties") }

func readServerProps() (map[string]string, error) {
	if serverDir() == "" {
		return nil, errors.New("sin MCBOT_SERVER_DIR")
	}
	return properties.Read(serverPropsPath())
}

// writeServerProps cambia esas claves y deja lo demás como estaba.
func writeServerProps(changes map[string]string) error {
	return properties.Update(serverPropsPath(), changes)
}

// ---- el estado ----

type serverPlayer struct {
	Name string `json:"name"`
	Op   bool   `json:"op"`
	Bot  bool   `json:"bot"`
}

type serverState struct {
	RCON       bool              `json:"rcon"`
	Files      bool              `json:"files"` // ve server.properties
	Online     bool              `json:"online"`
	Error      string            `json:"error,omitempty"`
	Players    []serverPlayer    `json:"players"`
	Max        int               `json:"max"`
	Difficulty string            `json:"difficulty"`
	MSPT       float64           `json:"mspt"` // ms por tick (50 = al límite)
	Seed       string            `json:"seed"`
	Whitelist  []string          `json:"whitelist"`
	Bans       []string          `json:"bans"`
	Ops        []string          `json:"ops"`
	Gamerules  map[string]string `json:"gamerules"`
	Props      map[string]string `json:"props"`
	PropDefs   []serverProp      `json:"prop_defs"`
	RuleDefs   any               `json:"rule_defs"`
	Pending    bool              `json:"pending"`       // hay cambios que piden reiniciar
	Restart    restartStatus     `json:"restart"`       // el reinicio en curso (restart.go)
	Power      string            `json:"power"`         // on, off, stopping, starting (power.go)
	Target     target            `json:"target"`        // a dónde está conectado el bot
	PowerSince float64           `json:"power_since"`   // segundos desde el último apagar/encender
	BootEst    float64           `json:"boot_estimate"` // lo que se calcula que tarda en arrancar (s)
}

var (
	reList  = regexp.MustCompile(`There are (\d+) of a max of (\d+) players online:?\s*(.*)`)
	reMSPT  = regexp.MustCompile(`Average time per tick: ([\d.]+)ms`)
	reSeed  = regexp.MustCompile(`Seed: \[(-?\d+)\]`)
	reRule  = regexp.MustCompile(`is currently set to: (\S+)`)
	reNames = regexp.MustCompile(`players?:\s*(.*)$`)
)

// serverPending: se guardaron propiedades que piden reiniciar el servidor.
var serverPending struct{ at time.Time }

func serverStatus() (st serverState) {
	st = serverState{RCON: rcon.Addr() != "", Files: serverDir() != "", PropDefs: serverProps, RuleDefs: gamerules,
		Gamerules: map[string]string{}, Players: []serverPlayer{}, Whitelist: []string{}, Bans: []string{}, Ops: []string{}}
	if st.Files {
		if p, err := readServerProps(); err == nil {
			st.Props = map[string]string{}
			for _, d := range serverProps {
				st.Props[d.Key] = p[d.Key]
			}
		} else {
			st.Files = false
		}
		st.Ops = jsonNames(filepath.Join(serverDir(), "ops.json"))
		st.Pending = !serverPending.at.IsZero()
	}
	st.Restart = currentRestart()
	st.Target = currentTarget()
	st.BootEst = round1(lastBootTook().Seconds())
	srvPower.Lock()
	if !srvPower.since.IsZero() {
		st.PowerSince = round1(time.Since(srvPower.since).Seconds())
	}
	srvPower.Unlock()
	if !st.RCON || st.Restart.Active && st.Restart.Phase != string(phaseWarn) || Off() && !pendingPower("apagar") {
		st.Power = PowerState(false)
		return st // (reiniciándose o apagado: la consola no está; no se le pregunta)
	}
	defer func() { st.Power = PowerState(st.Online) }()
	cmds := []string{"list", "difficulty", "tick query", "seed", "whitelist list", "banlist players"}
	for _, g := range gamerules {
		cmds = append(cmds, "gamerule "+g.Name)
	}
	out, err := rcon.Cmds(cmds...)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Online = true
	if m := reList.FindStringSubmatch(out[0]); m != nil {
		st.Max, _ = strconv.Atoi(m[2])
		ops := map[string]bool{}
		for _, o := range st.Ops {
			ops[strings.ToLower(o)] = true
		}
		for _, n := range splitNames(m[3]) {
			st.Players = append(st.Players, serverPlayer{Name: n, Op: ops[strings.ToLower(n)], Bot: IsBotName(n)})
		}
	}
	st.Difficulty = strings.ToLower(strings.TrimPrefix(out[1], "The difficulty is "))
	if m := reMSPT.FindStringSubmatch(out[2]); m != nil {
		st.MSPT, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reSeed.FindStringSubmatch(out[3]); m != nil {
		st.Seed = m[1]
	}
	if m := reNames.FindStringSubmatch(out[4]); m != nil && !strings.HasPrefix(out[4], "There are no") {
		st.Whitelist = splitNames(m[1])
	}
	for _, l := range strings.Split(out[5], "\n") { // "X was banned by Y: razón"
		if i := strings.Index(l, " was banned by"); i > 0 {
			st.Bans = append(st.Bans, strings.TrimSpace(l[:i]))
		}
	}
	if len(st.Bans) == 0 { // (todo en una línea, según la versión)
		for _, m := range regexp.MustCompile(`(\w{3,16}) was banned by`).FindAllStringSubmatch(out[5], -1) {
			st.Bans = append(st.Bans, m[1])
		}
	}
	for i, g := range gamerules {
		if m := reRule.FindStringSubmatch(out[6+i]); m != nil {
			st.Gamerules[g.Name] = m[1]
		}
	}
	return st
}

func splitNames(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func jsonNames(path string) []string {
	b, err := os.ReadFile(path)
	out := []string{}
	if err != nil {
		return out
	}
	var xs []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(b, &xs)
	for _, x := range xs {
		out = append(out, x.Name)
	}
	return out
}

// ---- cambios ----

// setServerProps valida y guarda; lo que tiene comando se aplica ya.
func setServerProps(in map[string]string) (restart bool, err error) {
	defs := map[string]serverProp{}
	for _, d := range serverProps {
		defs[d.Key] = d
	}
	old, err := readServerProps()
	if err != nil {
		return false, err
	}
	changes := map[string]string{}
	for k, v := range in {
		d, ok := defs[k]
		if !ok {
			return false, i18n.Errorf("%s cannot be changed from here", "%s no se puede cambiar desde aquí", k)
		}
		v = strings.TrimSpace(v)
		switch d.Kind {
		case "bool":
			if v != "true" && v != "false" {
				return false, &i18n.Error{EN: d.LabelEn + ": true or false", ES: d.Label + ": true o false"}
			}
		case "int":
			n, err := strconv.Atoi(v)
			if err != nil || n < d.Min || n > d.Max {
				return false, &i18n.Error{EN: fmt.Sprintf("%s: a number from %d to %d", d.LabelEn, d.Min, d.Max), ES: fmt.Sprintf("%s: un número de %d a %d", d.Label, d.Min, d.Max)}
			}
		case "choice":
			ok := false
			for _, o := range d.Opts {
				ok = ok || o == v
			}
			if !ok {
				return false, &i18n.Error{EN: d.LabelEn + ": " + strings.Join(d.Opts, ", "), ES: d.Label + ": " + strings.Join(d.Opts, ", ")}
			}
		case "text":
			if len(v) > 200 || !utf8.ValidString(v) || strings.ContainsAny(v, "\r\n") {
				return false, &i18n.Error{EN: d.LabelEn + ": one line, 200 characters at most", ES: d.Label + ": una línea, 200 caracteres como mucho"}
			}
		}
		if old[k] != v {
			changes[k] = v
		}
	}
	if len(changes) == 0 {
		return false, nil
	}
	if err := writeServerProps(changes); err != nil {
		return false, err
	}
	for k, v := range changes {
		d := defs[k]
		if d.Live == "" || rcon.Addr() == "" {
			restart = true
			continue
		}
		arg := v
		if k == "white-list" {
			arg = map[string]string{"true": "on", "false": "off"}[v]
		}
		if out, err := rcon.Cmd(fmt.Sprintf(d.Live, arg)); err != nil {
			restart = true
		} else if out != "" {
			log.Printf("[servidor] %s", out)
		}
	}
	log.Printf("[panel] server.properties: %s", describeChanges(changes))
	if restart {
		serverPending.at = time.Now()
	}
	return restart, nil
}

func describeChanges(m map[string]string) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ", ")
}

// Routes: /server… en mux (ok comprueba la clave del panel).
func Routes(mux *http.ServeMux, ok func(http.ResponseWriter, *http.Request) bool) {
	jsonOut := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	post := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return false
		}
		return ok(w, r)
	}
	mux.HandleFunc("/server", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			jsonOut(w, serverStatus())
		}
	})
	mux.HandleFunc("/server/props", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in map[string]string
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return
		}
		restart, err := setServerProps(in)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, err)
			return
		}
		jsonOut(w, map[string]bool{"restart": restart})
	})
	mux.HandleFunc("/server/gamerule", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in struct{ Name, Value string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return
		}
		kind, known := validGamerule[in.Name]
		_, nerr := strconv.Atoi(in.Value)
		if !known || (kind == "bool" && in.Value != "true" && in.Value != "false") || (kind == "int" && nerr != nil) {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "invalid gamerule or value", "gamerule o valor no válido")
			return
		}
		out, err := rcon.Cmd("gamerule " + in.Name + " " + in.Value)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadGateway, err)
			return
		}
		log.Printf("[servidor] %s", out)
		jsonOut(w, map[string]string{"out": out})
	})
	mux.HandleFunc("/server/restart", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		if serverDir() == "" {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "only with the local Docker server (MCBOT_SERVER_DIR): outside Docker, stop would just shut it down", "solo con el servidor local de Docker (MCBOT_SERVER_DIR): fuera de Docker, stop lo apagaría sin más")
			return
		}
		if Off() {
			i18n.HTTPMsg(w, r, http.StatusConflict, "the server is off: start it", "el servidor está apagado: enciéndelo")
			return
		}
		if err := restartServer(); err != nil {
			code := http.StatusBadGateway
			if err == errRestarting {
				code = http.StatusConflict
			}
			i18n.HTTPError(w, r, code, err)
			return
		}
		serverPending.at = time.Time{}
		jsonOut(w, currentRestart())
	})
	power := func(f func() error, after string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !post(w, r) {
				return
			}
			if err := f(); err != nil {
				i18n.HTTPError(w, r, http.StatusConflict, err)
				return
			}
			jsonOut(w, map[string]string{"power": after})
		}
	}
	mux.HandleFunc("/server/stop", power(stopServer, "stopping"))
	mux.HandleFunc("/server/start", power(Start, "starting"))
}
