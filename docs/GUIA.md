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

## 5. Actualizar

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
