package main

// Ejemplos de uso de cada orden, para docs/COMMANDS.md y docs/ORDENES.md
// (docs.go). Cada uno: la línea tal cual se escribe y, si hace falta, qué hace.

type example struct {
	line string
	note text
}

var commandExamples = map[string][]example{
	"follow": {{"/follow Steve", text{}}},
	"goto": {
		{"/goto Steve", text{"walk to Steve", "ir hasta Steve"}},
		{"/goto 120 64 -340", text{"walk to that block", "ir a ese bloque"}},
		{"/goto ~50 ~ ~", text{"50 blocks east of where it is", "50 bloques al este de donde está"}},
	},
	"fight": {
		{"/fight Alex", text{"fight Alex until one of them dies", "pelear con Alex hasta que uno muera"}},
		{"/fight in 100 200", text{"everyone within 8 blocks of 100 200", "todos a menos de 8 bloques de 100 200"}},
		{"/fight in 0 0 50 50", text{"everyone inside that square", "todos dentro de ese cuadrado"}},
	},
	"guard": {
		{"/guard Steve", text{"until /stop", "hasta /stop"}},
		{"/guard Steve 30", text{"for 30 minutes", "durante 30 minutos"}},
	},
	"excavate": {
		{"/excavate 10 70 10 30 50 30", text{"the box between those two corners", "la caja entre esas dos esquinas"}},
		{"/excavate ~ ~ ~ ~15 ~-20 ~15", text{"a 16×21×16 hole from where it stands", "un hoyo de 16×21×16 desde donde está"}},
		{"/excavate 0 60 0 15 40 15 exact", text{"exactly those blocks (no chunk fitting)", "justo esos bloques (sin ajustar a chunks)"}},
		{"/excavate chunk 0 64", text{"the whole chunk it stands in, y 0 to 64", "el chunk entero donde está, de y 0 a 64"}},
		{"/excavate chests 104 103 -224", text{"store the loot there (a whole wall of chests counts)", "guardar ahí lo que saque (cuenta la pared de cofres entera)"}},
		{"/excavate chests list", text{"show the chests", "ver los cofres"}},
		{"/excavate chests remove 104 103 -224", text{}},
		{"/excavate chests clear", text{"forget them all", "olvidarlos todos"}},
	},
	"chop": {
		{"/chop oak ~ ~ ~", text{"every oak within 24 blocks of where it stands", "todos los robles a menos de 24 bloques de donde está"}},
		{"/chop oak,birch 100 64 200", text{"oaks and birches around that point (y = the ground)", "robles y abedules alrededor de ese punto (y = el suelo)"}},
		{"/chop dark_oak,pale_oak ~ ~ ~", text{"big 2×2 trees too", "también los árboles grandes de 2×2"}},
		{"/chop crimson,warped ~ ~ ~", text{"Nether stems", "tallos del Nether"}},
	},
	"ask": {
		{"/ask mine 20 iron and bring it to me", text{"plain words: the local AI plans the commands", "con palabras normales: la IA local planea las órdenes"}},
		{"/ask ve a talar unos robles", text{}},
		{"/ask how are you doing?", text{"it just answers in the chat", "solo te contesta en el chat"}},
	},
	"inventory": {
		{"/inventory", text{}},
		{"/inventory grid", text{"laid out like the game screen", "como en la pantalla del juego"}},
	},
	"drop": {
		{"/drop", text{"everything, on the ground", "todo, al suelo"}},
		{"/drop diamond", text{"all its diamonds", "todos sus diamantes"}},
		{"/drop cobblestone 32", text{"32 cobblestone", "32 de piedra"}},
		{"/drop Steve", text{"everything, thrown to Steve", "todo, tirado a Steve"}},
		{"/drop diamond 5 Steve", text{"5 diamonds, thrown to Steve", "5 diamantes, tirados a Steve"}},
	},
	"stop":   {{"/stop", text{}}},
	"status": {{"/status", text{}}},
	"say": {
		{"/say hello everyone", text{}},
		{"hello everyone", text{"the same: text without / goes to the chat", "lo mismo: el texto sin / va al chat"}},
	},
	"cmd": {
		{"/cmd time set day", text{"needs the bot to be op", "el bot tiene que ser op"}},
		{"/cmd tp Steve", text{}},
	},
	"sprint": {{"/sprint off", text{"always walk", "andar siempre"}}, {"/sprint on", text{}}},
	"lang":   {{"/lang es", text{}}, {"/lang en", text{}}},
	"help":   {{"/help", text{}}},
	"quit":   {{"/quit", text{"disconnect and close the bot", "desconectar y cerrar el bot"}}},
}

// examplesDoc: el bloque de ejemplos de una orden ("" si no tiene).
func examplesDoc(name string, es bool) string {
	ex := commandExamples[name]
	if len(ex) == 0 {
		return ""
	}
	w := 0
	for _, e := range ex {
		w = max(w, len([]rune(e.line)))
	}
	out := map[bool]string{false: "**Examples**\n\n```\n", true: "**Ejemplos**\n\n```\n"}[es]
	for _, e := range ex {
		out += e.line
		if n := pick(e.note, es); n != "" {
			for i := len([]rune(e.line)); i < w+3; i++ {
				out += " "
			}
			out += "# " + n
		}
		out += "\n"
	}
	return out + "```\n\n"
}
