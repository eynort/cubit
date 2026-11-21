package mcserver

// Lo que el servidor de pruebas necesita saber del programa que lo usa (un
// bot, o el hub con varios): se lo pone él al arrancar.
var (
	// DataFile: la ruta de un archivo de datos (lo que tardó en arrancar y
	// en reiniciarse la última vez).
	DataFile = func(name string) string { return name }

	// IsBotName: n es uno de los bots (en el hub, cualquiera de los suyos).
	IsBotName = func(n string) bool { return false }

	// Bot: a dónde está puesto el bot y si está dentro ahora mismo.
	Bot = func() (addr string, connected bool) { return "", false }

	// OnUp: el servidor de pruebas acaba de encenderse (el bot vuelve a
	// entrar ya; el hub avisa a sus bots).
	OnUp = func() {}
)
