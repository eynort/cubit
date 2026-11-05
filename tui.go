package main

// La consola del bot: una terminal estilo chat de Minecraft. El log arriba,
// la línea de órdenes abajo, una barra de estado y, al escribir "/", la lista
// de órdenes con su uso y explicación, que se filtra mientras escribes.
//
//	Tab / Shift+Tab, ↑ / ↓   recorrer las sugerencias (comandos, jugadores, opciones)
//	Enter                    elegir la sugerencia resaltada; sin ninguna, ejecutar
//	Esc                      cerrar las sugerencias
//	↑ / ↓ (sin sugerencias)  historial
//	RePág / AvPág            desplazar el log
//	Ctrl+C                   salir

import (
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// logFile es donde se guarda todo el log (para pegarlo o revisarlo luego).
const logFile = "minecraft-bot.log"

// Fondo oscuro: así lipgloss no le pregunta el color a la terminal (algunas
// no contestan y la consola tardaría en aparecer).
var _ = func() bool { lipgloss.SetHasDarkBackground(true); return true }()

var (
	stPrompt   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)
	stBox      = lipgloss.NewStyle().Background(lipgloss.Color("#1E1E1E"))
	stItem     = lipgloss.NewStyle().Foreground(lipgloss.Color("#DDDDDD")).Background(lipgloss.Color("#1E1E1E"))
	stSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF55")).Background(lipgloss.Color("#3A3A3A")).Bold(true)
	stDesc     = lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A")).Background(lipgloss.Color("#1E1E1E"))
	stHint     = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")).Background(lipgloss.Color("#1E1E1E")).Italic(true)
	stStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#2B4A2B"))
	stEcho     = lipgloss.NewStyle().Foreground(lipgloss.Color("#55FFFF"))
)

const maxSuggestions = 8 // filas de sugerencias visibles

// suggestion es una opción del autocompletado.
type suggestion struct {
	value string // la línea entera si se elige ("" = solo una pista, no se elige)
	label string // lo que se ve
	desc  string // la explicación al lado
}

type (
	logMsg    string
	statusMsg string
)

type tui struct {
	in     textinput.Model
	vp     viewport.Model
	lines  []string // el log, tal cual
	shown  []string // el log ajustado al ancho de shownW (cada línea una vez, no todo en cada mensaje)
	shownW int
	sugg   []suggestion
	sel    int // la sugerencia resaltada; -1 ninguna
	status string
	w, h   int
	hist   []string
	hpos   int
	// browsing: se está recorriendo el historial con las flechas (hasta que se
	// escribe algo): las flechas siguen por él y no por las sugerencias
	browsing bool
	draft    string // lo que había escrito antes de subir al historial
	// exec manda una línea (nil: al bot); los tests la cambian
	exec func(line string)
}

// ui es la consola en marcha.
type ui struct {
	p    *tea.Program
	done chan struct{}
	file *os.File
}

// startUI abre la consola si la entrada y la salida son una terminal; si no
// (p.ej. con la entrada redirigida) devuelve nil y se usa la consola simple.
func startUI() *ui {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}
	in := textinput.New()
	in.Prompt = stPrompt.Render("> ")
	in.Placeholder = tr("type / to see the commands, or a chat message", "escribe / para ver las órdenes, o un mensaje para el chat")
	in.Focus()
	in.CharLimit = 256
	m := &tui{in: in, vp: viewport.New(80, 20), sel: -1}
	m.hist = loadHistory()
	m.hpos = len(m.hist)
	u := &ui{p: tea.NewProgram(m, tea.WithAltScreen()), done: make(chan struct{})}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		u.file = f
	}
	log.SetFlags(log.Ltime)
	var file io.Writer = sessionWriter()
	if u.file != nil {
		file = io.MultiWriter(u.file, sessionWriter())
	}
	log.SetOutput(&uiWriter{p: u.p, file: file})
	go func() {
		if _, err := u.p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "consola:", err)
		}
		close(u.done)
		appQuit() // cerrar la consola (Ctrl+C) es salir
	}()
	go func() { // barra de estado
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-u.done:
				return
			case <-t.C:
				if b := current.Load(); b != nil {
					u.p.Send(statusMsg(b.statusLine()))
				}
			}
		}
	}()
	return u
}

// Stop cierra la consola y devuelve el log a la terminal normal.
func (u *ui) Stop() {
	if u == nil {
		return
	}
	u.p.Quit()
	<-u.done
	var out io.Writer = os.Stderr
	if u.file != nil {
		out = io.MultiWriter(os.Stderr, u.file)
	}
	log.SetOutput(io.MultiWriter(out, sessionWriter()))
}

// uiWriter manda cada línea del log a la consola y al fichero.
type uiWriter struct {
	p    *tea.Program
	file io.Writer
	mu   sync.Mutex
	part string
}

func (w *uiWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil { // al archivo sin los colores
		_, _ = w.file.Write([]byte(stripANSI(string(b))))
	}
	w.part += string(b)
	for {
		i := strings.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		w.p.Send(logMsg(w.part[:i]))
		w.part = w.part[i+1:]
	}
	return len(b), nil
}

func (b *Bot) statusLine() string {
	if b.Player == nil {
		return ""
	}
	st, p := b.Player.Status(), b.Player.Position()
	task := ""
	if b.Tasks != nil {
		if t := b.Tasks.Status(); t != "" {
			task = "  🛠 " + t
		}
	}
	if b.Queue != nil {
		if q := b.Queue.Short(); q != "" {
			task += "  📋 " + q
		}
	}
	return fmt.Sprintf(" %s  ♥ %.0f  🍗 %d  📍 %.0f %.0f %.0f  ⚔ %s%s ", botName, st.Health, st.Food, p.X, p.Y, p.Z, b.Combat.Status(), task)
}

func (m *tui) Init() tea.Cmd { return textinput.Blink }

func (m *tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.in.Width = msg.Width - 3
		m.layout()
		return m, nil
	case logMsg:
		bottom := m.vp.AtBottom()
		m.addLine(string(msg))
		m.refresh()
		if bottom {
			m.vp.GotoBottom()
		}
		return m, nil
	case statusMsg:
		m.status = string(msg)
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyTab, tea.KeyShiftTab:
			m.cycle(map[bool]int{true: 1, false: -1}[msg.Type == tea.KeyTab])
			return m, nil
		case tea.KeyUp, tea.KeyDown:
			// como en una terminal: las flechas recorren el historial (con la
			// línea vacía o ya recorriéndolo); con sugerencias a la vista de
			// lo que se está escribiendo, las recorren a ellas (y Tab siempre)
			d := map[bool]int{true: -1, false: 1}[msg.Type == tea.KeyUp]
			if !m.browsing && m.in.Value() != "" && m.selectable() > 0 {
				m.cycle(d)
			} else {
				m.history(d)
			}
			return m, nil
		case tea.KeyEsc:
			m.sugg, m.sel = nil, -1
			m.layout()
			return m, nil
		case tea.KeyPgUp:
			m.vp.HalfViewUp()
			return m, nil
		case tea.KeyPgDown:
			m.vp.HalfViewDown()
			return m, nil
		case tea.KeyEnter:
			if m.sel >= 0 { // elegir la sugerencia
				m.setValue(m.sugg[m.sel].value)
				m.suggest()
				return m, nil
			}
			line := strings.TrimSpace(m.in.Value())
			m.setValue("")
			m.sugg, m.sel = nil, -1
			m.layout()
			if line == "" {
				return m, nil
			}
			if secret(line) {
				// (una contraseña no se guarda en el historial)
			} else if len(m.hist) == 0 || m.hist[len(m.hist)-1] != line { // sin repetir la última
				m.hist = append(m.hist, line)
				if len(m.hist) > historyMax {
					m.hist = m.hist[len(m.hist)-historyMax:]
				}
				saveHistory(m.hist)
			}
			m.hpos, m.browsing, m.draft = len(m.hist), false, ""
			m.addLine(stEcho.Render("> " + line))
			m.refresh()
			m.vp.GotoBottom()
			if m.exec != nil {
				m.exec(line)
			} else {
				go execLine(line) // las órdenes no bloquean la consola
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	before := m.in.Value()
	m.in, cmd = m.in.Update(msg)
	if m.in.Value() != before {
		m.browsing = false // escribió: deja de recorrer el historial
		m.hpos = len(m.hist)
		m.suggest()
	}
	return m, cmd
}

func (m *tui) setValue(v string) {
	m.in.SetValue(v)
	m.in.CursorEnd()
}

func (m *tui) selectable() int {
	n := 0
	for _, s := range m.sugg {
		if s.value != "" {
			n++
		}
	}
	return n
}

// cycle resalta la sugerencia siguiente (d=1) o anterior (d=-1) y la muestra en la línea.
func (m *tui) cycle(d int) {
	var idx []int // las que se pueden elegir
	for i, s := range m.sugg {
		if s.value != "" {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return
	}
	pos := -1
	for k, i := range idx {
		if i == m.sel {
			pos = k
		}
	}
	switch {
	case pos < 0 && d > 0:
		pos = 0
	case pos < 0:
		pos = len(idx) - 1
	default:
		pos = (pos + d + len(idx)) % len(idx)
	}
	m.sel = idx[pos]
	m.in.SetValue(strings.TrimRight(m.sugg[m.sel].value, " "))
	m.in.CursorEnd()
}

func (m *tui) history(d int) {
	if len(m.hist) == 0 {
		return
	}
	if !m.browsing {
		if d > 0 {
			return // abajo sin estar en el historial: nada
		}
		m.browsing, m.draft, m.hpos = true, m.in.Value(), len(m.hist)
	}
	m.hpos = max(0, min(len(m.hist), m.hpos+d))
	if m.hpos == len(m.hist) { // abajo del todo: lo que estaba escribiendo
		m.setValue(m.draft)
		m.browsing = false
		m.suggest()
		return
	}
	m.setValue(m.hist[m.hpos])
	m.sugg, m.sel = nil, -1 // recorriendo el historial, sin sugerencias por medio
	m.layout()
}

// El historial de órdenes se guarda entre sesiones (como el de una terminal).
const (
	historyFile = ".historial"
	historyMax  = 500
)

// secret: la línea lleva una contraseña (/login, /register…): no se guarda.
func secret(line string) bool {
	f := strings.Fields(strings.ToLower(line))
	if len(f) == 0 {
		return false
	}
	switch f[0] {
	case "/login", "/l", "/log", "/register", "/reg", "/changepassword", "/changepass", "/cp", "/unregister", "/premium":
		return true
	}
	return false
}

func loadHistory() []string {
	b, err := os.ReadFile(historyFile)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) > historyMax {
		out = out[len(out)-historyMax:]
	}
	return out
}

func saveHistory(h []string) {
	_ = os.WriteFile(historyFile, []byte(strings.Join(h, "\n")+"\n"), 0o600)
}

// suggest recalcula las sugerencias para lo escrito.
func (m *tui) suggest() {
	m.sugg, m.sel = suggestions(m.in.Value()), -1
	m.layout()
}

// suggestions es el autocompletado de una línea: órdenes al escribir "/", y
// para cada argumento jugadores conectados, opciones o una pista del uso.
func suggestions(v string) []suggestion {
	if !strings.HasPrefix(v, "/") {
		return nil
	}
	parts := strings.Split(v[1:], " ")
	if len(parts) == 1 { // la orden
		prefix := strings.ToLower(parts[0])
		var names []string
		for name := range commands {
			if strings.HasPrefix(name, prefix) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		var out []suggestion
		for _, name := range names {
			c := commands[name]
			val := "/" + name
			if len(c.args) > 0 {
				val += " "
			}
			out = append(out, suggestion{value: val, label: c.usage(name), desc: c.desc.String()})
		}
		if len(out) == 0 && prefix != "" {
			out = append(out, suggestion{label: "/" + parts[0], desc: tr("not a bot command: it will be sent to the server", "no es del bot: se mandará al servidor")})
		}
		return out
	}
	c, ok := commands[strings.ToLower(parts[0])]
	if !ok {
		return nil
	}
	return completeArgs(c, strings.ToLower(parts[0]), parts)
}

// layout reparte la altura entre el log, las sugerencias, la línea y la barra de estado.
func (m *tui) layout() {
	if m.w == 0 {
		return
	}
	m.vp.Width = m.w
	m.vp.Height = max(1, m.h-2-min(len(m.sugg), maxSuggestions))
	m.refresh()
}

// maxLines es cuántas líneas del log se guardan en pantalla.
const maxLines = 2000

// addLine añade una línea al log (ajustada al ancho, si ya se conoce).
func (m *tui) addLine(l string) {
	m.lines = append(m.lines, l)
	if m.shownW == m.w && len(m.shown) == len(m.lines)-1 {
		m.shown = append(m.shown, m.wrap(l))
	}
	if len(m.lines) > maxLines+maxLines/4 { // de vez en cuando, no en cada línea: se copia
		cut := len(m.lines) - maxLines
		m.lines = append(m.lines[:0:0], m.lines[cut:]...)
		if len(m.shown) >= cut {
			m.shown = append(m.shown[:0:0], m.shown[cut:]...)
		}
	}
}

func (m *tui) wrap(l string) string { return lipgloss.NewStyle().Width(max(1, m.w)).Render(l) }

func (m *tui) refresh() {
	bottom := m.vp.AtBottom()
	if m.shownW != m.w || len(m.shown) != len(m.lines) { // otro ancho: ajustar todo otra vez
		m.shown = make([]string, len(m.lines))
		for i, l := range m.lines {
			m.shown[i] = m.wrap(l)
		}
		m.shownW = m.w
	}
	m.vp.SetContent(strings.Join(m.shown, "\n"))
	if bottom {
		m.vp.GotoBottom()
	}
}

func (m *tui) View() string {
	if m.w == 0 {
		return "cargando…"
	}
	var b strings.Builder
	b.WriteString(m.vp.View())
	b.WriteString("\n")
	if len(m.sugg) > 0 {
		// una ventana de maxSuggestions filas alrededor de la resaltada
		start := 0
		if m.sel >= maxSuggestions {
			start = m.sel - maxSuggestions + 1
		}
		end := min(len(m.sugg), start+maxSuggestions)
		lw := 0
		for _, s := range m.sugg[start:end] {
			lw = max(lw, lipgloss.Width(s.label))
		}
		for i := start; i < end; i++ {
			s := m.sugg[i]
			label := fmt.Sprintf(" %-*s ", lw, s.label)
			var row string
			switch {
			case s.value == "":
				row = stHint.Render(label)
			case i == m.sel:
				row = stSelected.Render(label)
			default:
				row = stItem.Render(label)
			}
			row += stDesc.Render(" " + s.desc + " ")
			if pad := m.w - lipgloss.Width(row); pad > 0 {
				row += stBox.Render(strings.Repeat(" ", pad))
			}
			b.WriteString(row + "\n")
		}
	}
	b.WriteString(m.in.View() + "\n")
	st := m.status
	if pad := m.w - lipgloss.Width(st); pad > 0 {
		st += strings.Repeat(" ", pad)
	}
	b.WriteString(stStatus.Render(st))
	return b.String()
}
