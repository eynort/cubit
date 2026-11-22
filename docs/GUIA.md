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
- **Versión**: Minecraft Java **26.2** (protocolo 776), y otras versiones por ViaProxy (ver el apartado 5). Solo servidores vanilla: no usa comandos de plugins.
- **Juega limpio**: úsalo en tu servidor o en los que permiten bots.

## 5. Otras versiones de Minecraft

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
