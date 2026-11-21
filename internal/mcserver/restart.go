package mcserver

// El reinicio del servidor de Minecraft desde el panel, seguido de principio a
// fin para que la página pueda decir "de nuevo en línea en N segundos":
//
//	aviso      el "say" a los jugadores (warnDelay)
//	parando    "stop" mandado; la consola aún contesta
//	arrancando la consola no contesta (Docker lo está volviendo a arrancar)
//	           → en línea: la consola vuelve a contestar
//
// Cuánto tarda se mide cada vez y se guarda (datos/<servidor>/), así la
// siguiente cuenta atrás parte de lo que tardó la última.

import (
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cubit/internal/i18n"
	"cubit/internal/rcon"
)

const (
	warnDelay        = 5 * time.Second  // del aviso en el chat al "stop"
	defaultRestart   = 40 * time.Second // la primera vez (sin medida): aviso + parar + arrancar
	restartGiveUp    = 5 * time.Minute  // más que esto, se deja de seguir
	restartTimesFile = "reinicio_servidor.txt"
)

type restartPhase string

const (
	phaseWarn     restartPhase = "aviso"
	phaseStopping restartPhase = "parando"
	phaseStarting restartPhase = "arrancando"
)

var srvRestart struct {
	sync.Mutex
	since  time.Time // "" = no se está reiniciando
	phase  restartPhase
	expect time.Duration // lo que se calcula que tarda en total
	backAt time.Time     // cuándo volvió (para el "de nuevo en línea" un rato)
	took   time.Duration // lo que tardó esa vez
	failed string        // no volvió (se dejó de seguir)
}

// restartStatus: lo que ve la página (en /server).
type restartStatus struct {
	Active  bool    `json:"active"`
	Phase   string  `json:"phase,omitempty"`
	Elapsed float64 `json:"elapsed"`          // segundos desde que se pidió
	ETA     float64 `json:"eta"`              // segundos que faltan (estimado; 0 = ya debería)
	Expect  float64 `json:"expect"`           // la estimación total
	Late    bool    `json:"late,omitempty"`   // tarda más de lo estimado
	BackAgo float64 `json:"back_ago"`         // volvió hace tantos segundos (-1 = no ha vuelto)
	Took    float64 `json:"took,omitempty"`   // lo que tardó (al volver)
	Failed  string  `json:"failed,omitempty"` // no volvió a tiempo
}

func currentRestart() restartStatus {
	srvRestart.Lock()
	defer srvRestart.Unlock()
	st := restartStatus{BackAgo: -1, Failed: srvRestart.failed}
	if !srvRestart.backAt.IsZero() {
		st.BackAgo = round1(time.Since(srvRestart.backAt).Seconds())
		st.Took = round1(srvRestart.took.Seconds())
	}
	if srvRestart.since.IsZero() {
		return st
	}
	el := time.Since(srvRestart.since)
	st.Active, st.Phase = true, string(srvRestart.phase)
	st.Elapsed, st.Expect = round1(el.Seconds()), round1(srvRestart.expect.Seconds())
	st.ETA = round1(math.Max(0, (srvRestart.expect - el).Seconds()))
	st.Late = el > srvRestart.expect
	return st
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// lastRestartTook: lo que tardó el último reinicio (o defaultRestart).
func lastRestartTook() time.Duration {
	b, err := os.ReadFile(DataFile(restartTimesFile))
	if err != nil {
		return defaultRestart
	}
	s, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || s < 5 || s > restartGiveUp.Seconds() {
		return defaultRestart
	}
	return time.Duration(s * float64(time.Second))
}

// restartServer avisa, manda "stop" y sigue el reinicio hasta que la consola
// vuelve a contestar. Error si ya se está reiniciando o la consola no contesta.
func restartServer() error {
	srvRestart.Lock()
	if !srvRestart.since.IsZero() {
		srvRestart.Unlock()
		return errRestarting
	}
	srvRestart.Unlock()
	if _, err := rcon.Cmd("say El servidor se reinicia en 5 segundos"); err != nil {
		return err
	}
	srvRestart.Lock()
	srvRestart.since, srvRestart.phase, srvRestart.expect = time.Now(), phaseWarn, lastRestartTook()
	srvRestart.backAt, srvRestart.failed = time.Time{}, ""
	srvRestart.Unlock()
	log.Printf("[panel] reinicio el servidor de Minecraft (Docker lo vuelve a arrancar; la última vez tardó %s)", lastRestartTook().Round(time.Second))
	go watchRestart()
	return nil
}

var errRestarting = i18n.Errorf("the server is already restarting", "el servidor ya se está reiniciando")

func setPhase(p restartPhase) {
	srvRestart.Lock()
	srvRestart.phase = p
	srvRestart.Unlock()
}

func watchRestart() {
	time.Sleep(warnDelay)
	setPhase(phaseStopping)
	_, _ = rcon.Cmd("stop")
	start := time.Now()
	wentDown := false
	for time.Since(start) < restartGiveUp {
		time.Sleep(time.Second)
		_, err := rcon.Cmd("list")
		switch {
		case err != nil && !wentDown:
			wentDown = true
			setPhase(phaseStarting)
		case err == nil && wentDown:
			srvRestart.Lock()
			took := time.Since(srvRestart.since)
			srvRestart.since, srvRestart.backAt, srvRestart.took = time.Time{}, time.Now(), took
			srvRestart.Unlock()
			_ = os.WriteFile(DataFile(restartTimesFile), []byte(strconv.FormatFloat(took.Seconds(), 'f', 1, 64)+"\n"), 0o644)
			log.Printf("[panel] el servidor de Minecraft vuelve a estar en línea (tardó %s)", took.Round(time.Second))
			return
		}
	}
	srvRestart.Lock()
	srvRestart.since, srvRestart.failed = time.Time{}, "el servidor no ha vuelto en 5 minutos: mira sus logs (./up.sh logs)"
	srvRestart.Unlock()
	log.Print("[panel] el servidor de Minecraft no ha vuelto en 5 minutos")
}
