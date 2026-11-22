# Guía de Cubit

[English](GUIDE.md) · [Todas las órdenes](ORDENES.md)

## 1. Ponerlo en marcha

### Con Docker: el panel y un servidor de pruebas

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
./up.sh TuNombreDeMinecraft
```

Ese comando pone en marcha dos cosas:

- **El panel web**, desde donde manejas tus bots. El enlace que escribe ya lleva tu clave de acceso.
- **Un servidor de pruebas**: Minecraft Java 26.2 vanilla, contigo de op. Entra desde Minecraft: *Multijugador → Conexión directa → `localhost:25565`*.

| | |
|---|---|
| `./up.sh logs` | ver qué está pasando |
| `./up.sh down` | pararlo todo |
| `.env` | los puertos, tu nombre y si se enciende el servidor de pruebas |

### Sin Docker: solo el programa

Descarga el de tu sistema en [Releases](../../../releases) y ejecútalo:

```sh
./cubit --name MiBot --owner TuNombre play.ejemplo.net   # entrar a un servidor
./cubit --monitor :8080 play.ejemplo.net                 # lo mismo, con el panel web
./cubit --demo                                           # probarlo sin ningún servidor
```

En Linux y macOS, primero `chmod +x cubit`. La próxima vez, `./cubit` sin nada vuelve a entrar al último servidor.

En **Android**, instala Termux y usa el archivo `android-arm64`. Añade `--view 4 --memlimit 120` para que gaste menos.

## 2. El panel

- **Bot**: tus bots, cada uno con la cabeza de su skin. Entra en uno para arrancarlo o pararlo, cambiar sus ajustes o moverlo a otro servidor.
- **Cada bot tiene su propia consola.** Escribe ahí para controlarlo, con autocompletado:
  - `/follow TuNombre` es una orden para el bot;
  - `/time set day` es un comando del servidor (si el bot es op);
  - `hola`, sin barra, es un mensaje en el chat del juego.
  - `Tab` completa, y `↑` `↓` recorren el historial.
- **Servidores**: encender, apagar y reiniciar el servidor de pruebas, ver quién está conectado y añadir servidores de fuera. En *Mundo* puedes rehacer el mundo de pruebas con la semilla que quieras, o una al azar: el anterior queda guardado como `world.anterior`. La tarjeta de cada servidor te dice cómo entrar.

## 3. Controlar el bot

Puedes darle órdenes de dos formas:

| Desde | Cómo |
|---|---|
| El panel | escribe en la consola del bot: `/follow TuNombre` |
| El juego | mándasela por privado siendo su dueño: `/msg MiBot follow TuNombre` |

Las órdenes que más vas a usar:

| | |
|---|---|
| `/follow <jugador>` · `/goto <x> <y> <z>` | que te siga, o que vaya a un sitio |
| `/fight <jugador>` · `/guard <jugador>` | pelear con alguien, o protegerle |
| `/excavate <x1> <y1> <z1> <x2> <y2> <z2>` | vaciar esa caja |
| `/excavate chests <x> <y> <z>` | dónde guarda lo que saca y de dónde coge picos |
| `/inventory` · `/drop` | ver lo que lleva; dártelo |
| `/stop` | dejar lo que esté haciendo |

La lista completa está en [ORDENES.md](ORDENES.md). También la ves con `/help` en la consola.

## 4. Bueno saberlo

- **Dueños**: solo los dueños pueden darle órdenes desde el juego. Se ponen con `--owner A,B`, o en los ajustes del bot en el panel.
- **Servidores premium**: añade `--microsoft`. La primera vez te da un código para entrar con tu cuenta de Microsoft.
- **Memoria**: unos 70 MB por bot. `--view` (distancia de visión en chunks) y `--memlimit` (en MB) la bajan para móviles.
- **Versión**: solo Minecraft Java **26.2** (protocolo 776) y servidores vanilla: no usa comandos de plugins.
- **Juega limpio**: úsalo en tu servidor o en los que permiten bots.

## 5. Háblale (IA local)

Los dueños pueden escribirle frases normales en vez de órdenes, por privado (`/msg MiBot mina 20 de hierro y tráemelo`), por el chat nombrando al bot, o con `/ask <texto>`. Un modelo de IA pequeño que corre **en tu propio equipo** la lee, planea las órdenes (las de arriba y solo esas: nunca comandos del servidor ni `/quit`), las ejecuta, mira los resultados y te contesta en el chat como un colega. Las órdenes normales (`/follow Steve`) no pasan por la IA y siguen funcionando aunque esta falle.

Es gratis: sin cuenta, sin clave de API y nada sale de tu red. Usa [Ollama](https://ollama.com).

- **Con Docker (`./up.sh`)**: ya viene encendida. `up.sh` levanta un servicio `ollama` y baja el modelo la primera vez: unos **4,6 GB, una sola vez** (`./up.sh logs ollama-pull` enseña el progreso). Hasta que acabe, el bot dice que aún se está bajando el cerebro; el panel (ajustes del bot, *Asistente IA*) también enseña el progreso. Ahí mismo la apagas por bot o eliges otro modelo.
- **Sin Docker (programa suelto, Linux, macOS, Windows)**: instala Ollama y `ollama pull gemma4:e2b-it-q4_K_M`. El bot lo busca en `http://localhost:11434`; si está en otro sitio, usa `--ollama http://equipo:11434`, la variable `OLLAMA_URL` o la casilla *Dirección de Ollama* del panel. Si falta el modelo, el bot empieza a bajarlo solo.
- **Android (Termux)**: `pkg install ollama`, `ollama serve &`, `ollama pull gemma4:e2b-it-q4_K_M` y arranca el bot con la dirección por defecto. O deja Ollama en tu PC y apunta el bot del móvil con `--ollama http://<ip-del-pc>:11434`.

**Modelos y RAM, con franqueza.** El de por defecto, `gemma4:e2b-it-q4_K_M` (4,6 GB de descarga), necesita más o menos **3-4 GB de RAM libre** mientras contesta. `gemma4:e4b` es más listo pero pide más (unos 7 GB de descarga). `qwen3:4b` es otra opción. Un móvil de 800 MB no puede con un modelo útil: ahí, pon Ollama en otro equipo y usa `--ollama`, o apaga la IA. En un PC contesta en unos segundos; en un móvil, bastante más.

Ajustes (por bot, en el panel): encendida o no, modelo y dirección de Ollama. El bot recuerda 10 minutos los últimos intercambios con cada jugador, hace como mucho 6 pasos por frase y 60 frases por hora.
