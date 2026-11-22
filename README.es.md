<p align="center"><img src="brand/promo/hero-es.png" alt="Cubit — un bot de Minecraft que pelea, excava y se mueve" width="100%"></p>

<h3 align="center">Un bot de Minecraft que pelea, excava y se mueve.<br>Ligerísimo: <b>~70 MB de RAM por bot</b>.</h3>

<p align="center">
  <img alt="Minecraft Java 26.2, other versions via ViaProxy" src="https://img.shields.io/badge/Minecraft%20Java-26.2%20%2B%20ViaProxy-6b8f71?style=flat-square">
  <img alt="RAM por bot" src="https://img.shields.io/badge/RAM-~70%20MB%20por%20bot-4a5a6a?style=flat-square">
  <img alt="Programa" src="https://img.shields.io/badge/programa-17%20MB-5a6470?style=flat-square">
  <img alt="Licencia MIT" src="https://img.shields.io/badge/licencia-MIT-8a7f6a?style=flat-square">
</p>

<p align="center"><a href="README.md">English</a> · <a href="docs/GUIA.md"><b>Guía</b></a> · <a href="docs/ORDENES.md">Órdenes</a></p>

---

| | |
|---|---|
| **~70 MB** | de RAM por bot (75 MB en lo más fuerte de una pelea) |
| **0,7 GB** | diez bots a la vez |
| **17 MB** | un solo programa, sin Java, Node ni nada que instalar |

Corre en cualquier sitio: Linux, macOS, Windows, una Raspberry Pi, un VPS de 5 $ o un Android viejo.

## Qué hace

- **PvP.** Pelea como un experto: escudo, críticos y combos, la maza, cristales, perlas y tótems. Nunca huye.
- **Excavar.** Vacía zonas enteras capa a capa, guarda lo que saca en tus cofres y nunca rompe un pico.
- **Minar.** En ramas, como un jugador de verdad: baja por una escalera a la mejor capa para cada mineral, sin xray.
- **Talar.** Árboles enteros de las maderas que digas, también los de 2×2 y los del Nether.
- **Moverse.** Encuentra el camino solo con A*, y hace torres y puentes si le hacen falta.
- **Discord (opcional).** Da órdenes con `/bot`, `/bots` y `/status`, y lee en un canal lo que dicen tus bots. Mira la [guía](docs/GUIA.md#3-discord-opcional).
- **Háblale.** Los dueños pueden escribir frases normales ("mina 20 de hierro y tráemelo") y una IA local y gratuita (Ollama, sin cuentas ni claves) las convierte en órdenes. Opcional: se apaga por bot en el panel. Mira la [Guía](docs/GUIA.md#7-háblale-ia-local).

Todos tus bots se manejan desde un **panel web**. Ahí cada bot tiene su propia consola de órdenes, y el panel incluye un servidor de pruebas en el que puedes jugar al momento.

<p align="center"><img src="brand/promo/panel-bots-es.png" alt="El panel" width="100%"></p>

## Empieza en un minuto

**Windows**: con [Scoop](https://scoop.sh):

```powershell
scoop bucket add cubit https://github.com/eynort/cubit
scoop install cubit/cubit
```

Esto añade un acceso *Cubit* en el menú Inicio que abre el panel en tu navegador. O a mano: descarga `cubit-<versión>-windows-amd64.zip` en [Releases](../../releases), descomprímelo donde quieras y ejecuta `cubit.exe --hub :8080 --open` (crea una carpeta `data` junto a él para guardar ahí tus datos). No hay instalador: `cubit.exe` no está firmado, así que Windows SmartScreen puede avisarte la primera vez: pulsa **Más información → Ejecutar de todas formas**, o compruébalo con `SHA256SUMS`. Un paquete de `winget install` llegará más adelante, cuando el proyecto esté publicado ahí.

**Linux**: un solo comando, sin root:

```sh
curl -fsSL https://github.com/eynort/cubit/releases/latest/download/install.sh | sh
```

O instala el `.deb` / `.rpm` de [Releases](../../releases) (`sudo apt install ./cubit_*.deb`, `sudo dnf install ./cubit-*.rpm`). Para dejar el panel siempre encendido: `systemctl --user enable --now cubit`.

**macOS** y **Android (Termux)**: descarga el programa de tu sistema en [Releases](../../releases) y ejecuta `./cubit --hub :8080 --open`.

Después:

1. **Entra al panel**: *Cubit* en el menú de aplicaciones lo abre en el navegador. El enlace lleva una clave de acceso (`?t=…`): copia el enlace entero.
2. **Crea tu bot**: el panel te guía paso a paso (nombre, servidor, dueño, idioma).
3. **Juega con él**: la primera vez, el panel te ofrece preparar un servidor de pruebas. En **Minecraft Java 26.2**, ve a *Multijugador → Conexión directa* y pon `localhost:25565`.

¿Quieres compilarlo? Mira **[Compílalo tú mismo](docs/COMPILAR.md)**. Todo lo demás está en la **[Guía](docs/GUIA.md)**: cómo controlar el bot, el panel, los servidores y los móviles. Para manejar los bots desde tus propios scripts hay una **[API HTTP](docs/API.es.md)**.

El bot habla Minecraft Java 26.2; en servidores de **otras versiones** entra solo a través de [ViaProxy](docs/GUIA.md#6-otras-versiones-de-minecraft).

### Avanzado: Docker

Si prefieres contenedores (así también se encienden el servidor de pruebas y Ollama), necesitas [Docker](https://docs.docker.com/get-docker/):

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
./up.sh TuNombreDeMinecraft
```

Al terminar, te escribe el enlace de tu panel (`http://localhost:8080/?t=…`) y la dirección de Minecraft (`localhost:25565`).

## Instrucciones para usar el bot

Escribe las órdenes en la **consola del bot en el panel**. En el juego, mándaselas por privado siendo su dueño: `/msg MiBot follow Steve`. `~` es "donde está el bot", como en Minecraft.

**`/follow <jugador>`**: sigue a un jugador sin parar, hasta `/stop`.
```
/follow Steve
```

**`/goto <jugador>` · `/goto <x> <y> <z>`**: va hasta un jugador o hasta un bloque, y hace torres y puentes si le hacen falta. Si ese bloque está en el aire o bajo tierra, se para en el suelo más cercano de esa columna.
```
/goto Steve
/goto 120 64 -340
/goto ~50 ~ ~     → 50 bloques al este de donde está
```

**`/fight <jugador>` · `/fight in <x> <z> [<x2> <z2>]`**: pelea con un jugador hasta que uno de los dos muere, o con todo el que entre en una zona (un punto tiene un radio de 8 bloques, o da dos esquinas).
```
/fight Alex
/fight in 100 200             → todos a menos de 8 bloques de 100 200
/fight in 0 0 50 50           → todos dentro de ese cuadrado
```

**`/guard <jugador> [<minutos>]`**: modo guardaespaldas. Sigue a un jugador y ataca a quien le pegue.
```
/guard Steve
/guard Steve 30               → durante 30 minutos
```

**`/excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact]`**: vacía la caja entera entre dos esquinas, de 4 en 4 bloques desde arriba. Nunca rompe un pico.
```
/excavate 10 70 10 30 50 30   → la caja entre esas dos esquinas
/excavate ~ ~ ~ ~15 ~-20 ~15  → un hoyo de 16×21×16 desde donde está
/excavate 0 60 0 15 40 15 exact → justo esos bloques (sin exact, ajusta la caja a chunks enteros)
/excavate chunk 0 64          → el chunk entero donde está, de la altura 0 a la 64
```

**`/excavate chests …`**: los cofres donde `/excavate` guarda lo valioso cuando se llena y de donde coge picos. Si le das uno de una pared de cofres, cuenta la pared entera.
```
/excavate chests 104 103 -224 → apuntar ese cofre
/excavate chests list         → verlos
/excavate chests remove 104 103 -224
/excavate chests clear        → olvidarlos todos
```

**`/mine <minerales> <x> <y> <z> [every <n>] [total <n>]`**: mina en ramas como un jugador de verdad, sin xray. Baja por una escalera a la mejor capa para ese mineral (diamante en y -59, hierro en 16…), hace un túnel con ramas cada 3 bloques y solo pica los minerales que de verdad ve. `x y z` es el cofre donde deja lo minado (cada 8 por defecto) y de donde coge picos. La mina empieza donde está el bot.
```
/mine diamond 104 64 -224              → diamantes, a ese cofre cada 8
/mine iron,copper ~ ~ ~ every 16       → una capa que vale para los dos, cofre donde está
/mine diamond 104 64 -224 total 32     → parar al llegar a 32
```

**`/chop <maderas> <x> <y> <z>`**: tala los árboles enteros de esas maderas a menos de 24 bloques de un punto, con la copa, y recoge los troncos. No toca construcciones (troncos sin hojas naturales, o pegados a tablones, cristal, puertas…). La `y` es la del suelo.
```
/chop oak ~ ~ ~                  → todos los robles de alrededor
/chop oak,birch 100 64 200       → robles y abedules alrededor de ese punto
/chop dark_oak,pale_oak ~ ~ ~    → también los árboles grandes de 2×2
/chop crimson,warped ~ ~ ~       → tallos del Nether
```

**`/afk [here | <x> <y> <z>]`**: se queda en un sitio las 24 horas hasta `/stop` (donde está, o primero va andando a las coordenadas). Cada 1-4 minutos hace algo pequeño (mirar alrededor, saltar, dar un paso y volver) para que el servidor no lo eche por inactivo, y vuelve al sitio si lo empujan. Come con hambre, se defiende de los mobs que se acercan, duerme en una cama a menos de 16 bloques de noche o con tormenta, y tras morir o reconectar vuelve andando al sitio.
```
/afk                          → quedarse aquí mismo
/afk 120 64 -340              → ir allí y quedarse
```

**`/inventory [grid]`**: enseña lo que lleva: armadura, barra e inventario.
```
/inventory
/inventory grid               → colocado como en la pantalla del juego
```

**`/drop [<objeto> [<cantidad>]] [<jugador>]`**: tira todo, o un objeto, o va hasta un jugador y se lo tira.
```
/drop                         → todo, al suelo
/drop diamond                 → todos sus diamantes
/drop cobblestone 32          → 32 de piedra
/drop Steve                   → todo, tirado a Steve
/drop diamond 5 Steve         → 5 diamantes, tirados a Steve
```

**`/queue <orden>` · `<a> ; <b>`**: una lista de tareas. `/queue` apunta una orden (`chop`, `mine`, `excavate` o `goto x y z`) para cuando acabe la tarea actual. En cualquier línea, ` ; ` encadena órdenes: la primera empieza ya y las demás esperan su turno. Si una tarea falla o se rinde, el bot lo dice en el chat y pausa la lista; `/queue resume` sigue. La lista sobrevive a las reconexiones.
```
/queue /chop oak ~ ~ ~
/queue list                   → lo que espera, numerado
/queue remove 2               → olvidar la segunda
/queue clear                  → olvidarlas todas
/queue resume                 → seguir tras un fallo o un /stop
/mine iron ~ ~ ~ ; /chop oak 10 64 10   → minar ya, talar después
```

**`/stop`**: deja lo que esté haciendo: pelear, andar o excavar. También pausa la cola (guarda lo que espera; `/queue resume` sigue).
```
/stop
```

**`/status`**: vida, comida, posición y con quién pelea.
```
/status
```

**`/say <texto>`** (o escribe sin `/`): escribe en el chat del juego.
```
/say hola a todos
hola a todos
```

**`/cmd <comando>`**: ejecuta un comando del servidor como el bot (para la mayoría tiene que ser op).
```
/cmd time set day
/cmd tp Steve
```

**`/sprint on|off`**: le deja correr, o hace que ande siempre.
```
/sprint off
```

**`/lang en|es`**: el idioma de sus mensajes. Las órdenes son siempre en inglés.
```
/lang es
```

**`/help`**: la lista de órdenes. **`/quit`**: desconecta el bot y lo cierra.

La referencia completa, con todas las opciones, está en [docs/ORDENES.md](docs/ORDENES.md).

---

<sub>MIT. No es un producto oficial de Minecraft. No está aprobado por Mojang ni Microsoft, ni asociado con ellos.</sub>
