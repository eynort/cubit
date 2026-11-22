# Guía de Cubit

[English](GUIDE.md) · [Todas las órdenes](ORDENES.md)

## 1. Ponerlo en marcha

### Instalarlo (sin Docker ni clonar)

- **Windows**: `scoop bucket add cubit https://github.com/eynort/cubit` y luego `scoop install cubit/cubit`. Añade un acceso *Cubit* en el menú Inicio que abre el panel; tus datos se quedan en la carpeta `persist\cubit\data` de Scoop al actualizar. O descarga `cubit-<versión>-windows-amd64.zip` (o `arm64`) en [Releases](../../../releases), descomprímelo y ejecuta `cubit.exe --hub :8080 --open`; una carpeta `data` junto a `cubit.exe` guarda ahí tus datos (modo portátil), si no usa la carpeta actual o `--data`. No hay instalador, y `cubit.exe` no está firmado, así que SmartScreen puede avisarte: **Más información → Ejecutar de todas formas**. winget llegará más adelante.
- **Linux**: `curl -fsSL https://github.com/eynort/cubit/releases/latest/download/install.sh | sh` lo instala en `~/.local/bin` (comprueba el SHA256), o usa el `.deb` / `.rpm`. Abre el panel desde el menú de aplicaciones, o déjalo siempre encendido con `systemctl --user enable --now cubit`. Se quita con `sh install.sh --uninstall`. Los datos están en `~/.local/share/cubit`.
- **macOS / Android (Termux)**: descarga el programa en Releases y ejecuta `./cubit --hub :8080 --open`.

Por defecto, el panel solo se abre desde el equipo donde corre Cubit. Para abrirlo desde el móvil u otro ordenador de tu red (o en un VPS), usa `--hub 0.0.0.0:8080`. El panel va por HTTP sin cifrar, así que cualquiera de esa red que vea el enlace con su clave puede entrar: hazlo solo en redes de confianza, o ponlo detrás de un proxy HTTPS o de una VPN como Tailscale.

La primera vez, el panel te ofrece preparar un servidor de pruebas (y la IA local). Para elegir dónde van los datos, usa `--data <carpeta>` (o `CUBIT_DATA`); `--open` abre el panel en el navegador, o solo lo abre si ya hay un hub en marcha.

Para compilarlo tú mismo, mira [COMPILAR.md](COMPILAR.md).

### Avanzado: Docker, el panel y un servidor de pruebas

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

### Un servidor de pruebas sin Docker

Con el panel en modo hub (`./cubit --hub :8080`) y sin Docker, abre **Servidores → Servidor de pruebas**: te ofrece instalar uno. Marcas *Acepto la EULA de Minecraft* (<https://aka.ms/MinecraftEULA>, nunca se acepta por ti) y pulsas **Instalar**. Cubit descarga, comprobando cada suma:

- **Java**: Eclipse Temurin (JRE) de la API oficial de Adoptium, para tu sistema y la versión que pide Minecraft 26.2 (unos 50 MB);
- **el servidor**: el `server.jar` oficial de Mojang (unos 60 MB).

Todo queda en `server/` dentro de la carpeta de datos del hub (unos 250 MB). El progreso se ve en el panel, y si se corta la conexión, al pulsar de nuevo sigue donde se quedó. Luego el propio hub arranca el servidor (modo sin premium, puerto 25565, distancia de visión 8, RCON solo para el panel con una contraseña al azar), y *Encender / Apagar / Reiniciar / Mundo nuevo* funcionan igual que con Docker. Al cerrar el hub se para limpiamente. Lo último que ha dicho está en `GET /server/logs` y en `server/logs/latest.log`.

| | |
|---|---|
| `MCBOT_SERVER_XMX=2G` | memoria del servidor (`1G` por defecto) |
| `MCBOT_TEST_PORT` / `MCBOT_SERVER_RCON_PORT` | puerto de juego (25565) / de RCON (25575) |
| `MCBOT_NO_NATIVE=1` | no ofrecer nunca el servidor nativo |

Se elige solo: si hay `MCBOT_SERVER_DIR` o `MCBOT_RCON` (lo que pone `compose.yaml`), se usa Docker y nada cambia. El puerto de RCON escucha en todas las interfaces, protegido por esa contraseña: ciérralo en tu cortafuegos si el equipo no es solo tuyo.

## 2. El panel

- **Bot**: tus bots, cada uno con la cabeza de su skin. Entra en uno para arrancarlo o pararlo, cambiar sus ajustes o moverlo a otro servidor.
- **Cada bot tiene su propia consola.** Escribe ahí para controlarlo, con autocompletado:
  - `/follow TuNombre` es una orden para el bot;
  - `/time set day` es un comando del servidor (si el bot es op);
  - `hola`, sin barra, es un mensaje en el chat del juego.
  - `Tab` completa, y `↑` `↓` recorren el historial.
- **Servidores**: encender, apagar y reiniciar el servidor de pruebas, ver quién está conectado y añadir servidores de fuera. En *Mundo* puedes rehacer el mundo de pruebas con la semilla que quieras, o una al azar: el anterior queda guardado como `world.anterior`. La tarjeta de cada servidor te dice cómo entrar.

## 3. Discord (opcional)

Viene apagado. Al activarlo, el hub conecta un bot de Discord tuyo para que puedas dar órdenes a tus bots de Cubit desde Discord y ver lo que dicen. Todo corre dentro del hub; no se envía nada a ningún otro sitio.

1. Entra en <https://discord.com/developers/applications> y pulsa **New Application**.
2. Abre **Bot**, pulsa **Reset Token** y cópialo. Trátalo como una contraseña.
3. No hace falta ningún *Privileged Gateway Intent*: déjalos todos desactivados. El bot solo usa órdenes con barra.
4. Abre **OAuth2 → URL Generator**, marca los ámbitos `bot` y `applications.commands`, y en *Bot permissions* marca **View Channels** y **Send Messages**. Abre la URL que sale y añade el bot a tu servidor.
5. En Discord activa *Ajustes → Avanzado → Modo desarrollador*. Luego haz clic derecho en el canal que quieras como puente y elige **Copiar ID del canal**, y clic derecho en cada persona que pueda dar órdenes y elige **Copiar ID de usuario**.
6. En el panel abre **Servidores**, busca la tarjeta **Discord** y pulsa **Configurar**. Pega el token, el ID del canal y los IDs de usuario (separados por comas), actívalo y guarda.

La tarjeta muestra el estado de la conexión (conectando, conectado, sin conexión); si se cae, reintenta sola con una espera cada vez mayor.

- `/bot <nombre> <orden>` ejecuta una orden de la consola en ese bot, p. ej. `/bot Rocky /goto 100 64 200`.
- `/bots` lista tus bots y qué hace cada uno. `/status <nombre>` enseña uno.
- Solo pueden usarlas los IDs de usuario que pusiste; a los demás les sale una negativa amable que solo ven ellos.
- El puente publica lo que los bots dicen en el chat del juego (tarea acabada, atascado, falta un pico, ha muerto...) y lo que les dicen los jugadores, con el nombre del bot delante, en lotes de un mensaje por segundo como mucho.

El token solo se guarda en `hub.json` del equipo que ejecuta el hub (está fuera de git). El panel solo enseña sus 4 últimas letras y no sale nunca en los logs.

## 4. Controlar el bot

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
| `/queue <orden>` · `a ; b` | una lista de tareas: van una detrás de otra |
| `/stop` | dejar lo que esté haciendo (y pausar la cola) |

La lista completa está en [ORDENES.md](ORDENES.md). También la ves con `/help` en la consola.

Para manejar los bots desde scripts u otros programas (listarlos, mandarles órdenes, seguir el chat y las tareas en directo), mira la [API HTTP](API.es.md).

## 5. Bueno saberlo

- **Dueños**: solo los dueños pueden darle órdenes desde el juego. Se ponen con `--owner A,B`, o en los ajustes del bot en el panel.
- **Inicio de sesión del servidor (AuthMe / nLogin)**: si el servidor pide `/login` o `/register`, pon la contraseña en los ajustes del bot en el panel ("Contraseña de acceso al servidor"); el bot contesta solo, con avisos en inglés o en español. Se guarda solo en `bot.json` y no se vuelve a mostrar ni se escribe en el log.
- **Servidores premium**: añade `--microsoft`. La primera vez te da un código para entrar con tu cuenta de Microsoft.
- **Memoria**: unos 70 MB por bot. `--view` (distancia de visión en chunks) y `--memlimit` (en MB) la bajan para móviles.
- **Versión**: Minecraft Java **26.2** (protocolo 776), y otras versiones por ViaProxy (ver el apartado 6). Solo servidores vanilla: no usa comandos de plugins.
- **Juega limpio**: úsalo en tu servidor o en los que permiten bots.

## 6. Otras versiones de Minecraft

El bot habla solo Minecraft Java **26.2**. Para entrar en un servidor de otra versión (más vieja o más nueva) pasa por [ViaProxy](https://github.com/ViaVersion/ViaProxy), que traduce entre versiones. Es transparente: no hay que configurar nada por servidor.

Cómo funciona: antes de entrar, el bot pregunta la versión al servidor (el mismo ping de la lista multijugador). Si es la misma, entra directo. Si no, entra por ViaProxy, diciéndole a qué servidor y con qué versión. Un solo ViaProxy sirve para todos tus servidores. El panel enseña la versión detectada de cada servidor en la lista («por ViaProxy (versión X)», o «hace falta ViaProxy» si no hay).

**Docker (`./up.sh`)**: añade el perfil en `.env` y vuelve a arrancar.

```
COMPOSE_PROFILES=local,viaproxy      # o solo viaproxy si no quieres el servidor de pruebas
```

Construye `Dockerfile.viaproxy` (ViaProxy 3.4.14 sobre Java 21, unos 200 MB de RAM) y el hub ya apunta a él (`MCBOT_VIAPROXY=viaproxy:25568`). No se publica fuera de la red de Docker.

**Sin Docker**: baja `ViaProxy-3.4.14.jar` de las [releases](https://github.com/ViaVersion/ViaProxy/releases) (Java 17 o más; el jar `+java8` para Java más viejo) y dale al hub su ruta (lo arranca él solo, en `127.0.0.1:25568`, solo cuando un servidor lo necesita) o la dirección de uno que lleves tú:

```
./cubit --hub=:8080 --viaproxy /ruta/ViaProxy-3.4.14.jar
./cubit --hub=:8080 --viaproxy 127.0.0.1:25568     # uno tuyo, arrancado con:
java -jar ViaProxy.jar cli --bind-address 127.0.0.1:25568 --wildcard-domain-handling public --auth-method none
```

Un bot suelto (sin hub) acepta el mismo `--viaproxy host:puerto`, o la variable `MCBOT_VIAPROXY`.

**Servidores premium (online-mode)**: por ViaProxy el bot no entra con su cuenta de Microsoft; entra ViaProxy con **su** cuenta, la misma para todos los bots. Pon `VIAPROXY_AUTH=account` (Docker) o arráncalo con `--auth-method account`, y añade la cuenta una vez desde la consola de ViaProxy: `account add microsoft` (da un código para abrir en el navegador) y `account select 0`. En Docker: `docker compose attach viaproxy`. Los servidores sin premium no necesitan nada.

Límites:
- Todos los bots en servidores premium por ViaProxy juegan con esa única cuenta (un nombre, una conexión a la vez por servidor).
- Un servidor con registro SRV y sin puerto en la dirección: escribe el `host:puerto` real.
- Lo que no existe en la versión del servidor puede no funcionar (el bot da por hecho el comportamiento de 26.2). Los servidores muy viejos (antes de 1.13) son los más frágiles.
- Usa ViaProxy solo en redes de confianza: acepta cualquier destino de quien llegue a él.

## 7. Háblale (IA local)

Los dueños pueden escribirle frases normales en vez de órdenes, por privado (`/msg MiBot mina 20 de hierro y tráemelo`), por el chat nombrando al bot, o con `/ask <texto>`. Un modelo de IA pequeño que corre **en tu propio equipo** la lee, planea las órdenes (las de arriba y solo esas: nunca comandos del servidor ni `/quit`), las ejecuta, mira los resultados y te contesta en el chat como un colega. Las órdenes normales (`/follow Steve`) no pasan por la IA y siguen funcionando aunque esta falle.

Es gratis: sin cuenta, sin clave de API y nada sale de tu red. Usa [Ollama](https://ollama.com).

- **Con Docker (`./up.sh`)**: ya viene encendida. `up.sh` levanta un servicio `ollama` y baja el modelo la primera vez: unos **4,6 GB, una sola vez** (`./up.sh logs ollama-pull` enseña el progreso). Hasta que acabe, el bot dice que aún se está bajando el cerebro; el panel (ajustes del bot, *Asistente IA*) también enseña el progreso. Ahí mismo la apagas por bot o eliges otro modelo.
- **Sin Docker (programa suelto, Linux, macOS, Windows)**: instala Ollama y `ollama pull gemma4:e2b-it-q4_K_M`. El bot lo busca en `http://localhost:11434`; si está en otro sitio, usa `--ollama http://equipo:11434`, la variable `OLLAMA_URL` o la casilla *Dirección de Ollama* del panel. Si falta el modelo, el bot empieza a bajarlo solo.
- **Android (Termux)**: `pkg install ollama`, `ollama serve &`, `ollama pull gemma4:e2b-it-q4_K_M` y arranca el bot con la dirección por defecto. O deja Ollama en tu PC y apunta el bot del móvil con `--ollama http://<ip-del-pc>:11434`.

**Modelos y RAM, con franqueza.** El de por defecto, `gemma4:e2b-it-q4_K_M` (4,6 GB de descarga), necesita más o menos **3-4 GB de RAM libre** mientras contesta. `gemma4:e4b` es más listo pero pide más (unos 7 GB de descarga). `qwen3:4b` es otra opción. Un móvil de 800 MB no puede con un modelo útil: ahí, pon Ollama en otro equipo y usa `--ollama`, o apaga la IA. En un PC contesta en unos segundos; en un móvil, bastante más.

Ajustes (por bot, en el panel): encendida o no, modelo y dirección de Ollama. El bot recuerda 10 minutos los últimos intercambios con cada jugador, hace como mucho 6 pasos por frase y 60 frases por hora.

## 8. Actualizar

El hub mira en GitHub si hay una versión nueva al arrancar y luego cada 24 horas (no manda nada tuyo: una petición normal, sin cuenta ni identificadores). Si la hay, el panel enseña **Actualización disponible** con un enlace a las notas de la versión. Puedes apagar la comprobación, o hacerla ya, en *Bot* → *Buscar actualizaciones*.

Lo que ofrece el aviso depende de cómo instalaste Cubit:

| Instalado con | Se actualiza con |
|---|---|
| El script de instalación, o el zip (Linux, Windows, Termux) | **Actualizar ahora** en el panel (abajo) |
| `.deb` | descarga el `.deb` nuevo de [Releases](../../../releases) y `sudo apt install ./cubit_<versión>_<arq>.deb` |
| `.rpm` | descarga el `.rpm` nuevo y `sudo dnf install ./cubit_<versión>_<arq>.rpm` |
| Scoop | `scoop update cubit` |
| winget | `winget upgrade Cubit` |
| Docker (`up.sh`) | `./up.sh update`: `git pull --ff-only`, reconstruye solo la imagen del bot y reinicia solo el hub (los bots vuelven a entrar a los pocos segundos; el mundo de Minecraft no se toca) |

**Actualizar ahora** descarga el archivo de tu sistema, comprueba su SHA-256 con `SHA256SUMS` y la firma ed25519 de `SHA256SUMS` con la clave pública que lleva Cubit. Solo entonces copia `hub.json` y cada `bots/*/bot.json` a `backups/<versión anterior>/`, cambia el binario (el viejo queda al lado como `cubit.old`) y reinicia el hub y sus bots. Los bots que estaban trabajando retoman su tarea al volver (el panel avisa de cuántos trabajan antes de confirmar).

Si la versión nueva no arranca (o no responde en 60 segundos), el siguiente arranque devuelve `cubit.old` y el panel cuenta que la actualización se deshizo. Si la carpeta del binario no admite escritura, el panel solo enlaza la página de la versión.

Actualizar a mano: cambia el binario por el de [Releases](../../../releases). Los archivos de configuración llevan un número `"schema"` y se migran solos; uno escrito por un Cubit más nuevo no se toca: para volver atrás, restaura `backups/<versión>/`.
