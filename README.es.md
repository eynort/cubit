<p align="center"><img src="brand/promo/hero-es.png" alt="Cubit — un bot de Minecraft que pelea, excava y se mueve" width="100%"></p>

<h3 align="center">Un bot de Minecraft que pelea, excava y se mueve.<br>Ligerísimo: <b>~70 MB de RAM por bot</b>.</h3>

<p align="center">
  <img alt="Minecraft Java 26.2" src="https://img.shields.io/badge/Minecraft%20Java-26.2-6b8f71?style=flat-square">
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
- **Moverse.** Encuentra el camino solo con A*, y hace torres y puentes si le hacen falta.

Todos tus bots se manejan desde un **panel web**. Ahí cada bot tiene su propia consola de órdenes, y el panel incluye un servidor de pruebas en el que puedes jugar al momento.

<p align="center"><img src="brand/promo/panel-bots-es.png" alt="El panel" width="100%"></p>

## Empieza en un minuto

Necesitas [Docker](https://docs.docker.com/get-docker/).

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
./up.sh TuNombreDeMinecraft
```

Al terminar, te escribe el enlace de tu panel:

```
  Panel:      http://localhost:8080/?t=…
  Minecraft:  localhost:25565
```

1. **Entra al panel**: pega el enlace `Panel` en tu navegador. La parte `?t=…` es tu clave de acceso, así que copia el enlace entero.
2. **Crea tu bot**: el panel te guía paso a paso (nombre, servidor, dueño, idioma).
3. **Juega con él**: en **Minecraft Java 26.2**, ve a *Multijugador → Conexión directa* y pon `localhost:25565`.

También puedes usar el programa suelto, sin Docker: descárgalo en [Releases](../../releases). Todo lo demás está en la **[Guía](docs/GUIA.md)**: cómo controlar el bot, el panel, los servidores y los móviles.

---

<sub>MIT. No es un producto oficial de Minecraft. No está aprobado por Mojang ni Microsoft, ni asociado con ellos.</sub>
