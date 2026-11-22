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
