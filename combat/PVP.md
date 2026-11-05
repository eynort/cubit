# PvP: mecánicas y estrategia del bot

Referencia de lo que cuenta en una pelea en Minecraft Java (combate de la 1.9 en
adelante, el de 26.2) y de cómo lo usa el módulo `combat`. Cada regla del código
cita aquí su porqué. Las medidas son del simulador (`simworld`) con un retraso
de red de 4 ticks (200 ms por sentido), parecido al del servidor real.

## 1. Mecánicas (vanilla)

### Golpe
- **Carga del golpe** (`attackStrengthTicker`): el daño es `0.2 + 0.8·c²` del
  arma, con `c` de 0 a 1. Se carga en `20 / velocidad` ticks: espada 12.5,
  hacha de piedra o hierro 25, puño 5. **Cambiar lo que hay en la mano reinicia la
  carga** (seleccionar otra casilla, la tecla F): un cambio a mitad del choque es
  un golpe perdido.
- **Crítico** ×1.5: carga > 0.9, **cayendo** (no en el suelo, distancia de
  caída > 0), sin esprintar, sin estar en agua, escalera ni ceguera.
- **Empuje**: todo golpe empuja 0.4. **En sprint y cargado, un nivel más
  (+0.5) y el sprint se corta**: hay que volver a esprintar para el siguiente
  (W-tap: soltar W un par de ticks).
- **Barrido**: espada, en el suelo, sin sprint, cargado y sin crítico: daña a los
  de alrededor.
- **Alcance**: 3 bloques de los ojos a la caja del otro (el servidor lo mide con
  sus posiciones, que no son las que ve el cliente por el retraso).
- **Invulnerabilidad tras un golpe**: 20 ticks; en los 10 primeros un golpe
  solo cuenta si es mayor, y entonces solo la diferencia.

### Escudo
- Bloquea **todo** el daño que viene de delante (90° a cada lado de la mirada):
  golpes, flechas, explosiones. Tarda **5 ticks** en estar activo tras
  levantarlo. Con él arriba se va al 20 % de velocidad y no se puede ni correr
  ni pegar.
- A quien golpea un escudo se le empuja (0.5).
- **Un hachazo con el escudo arriba lo desactiva 5 s** (el servidor manda un
  *cooldown* del escudo).

### Vida y curación
- **Tótem** (en cualquier mano): salva de una muerte. Deja 1 de vida, da
  regeneración II (45 s), absorción II (5 s) y resistencia al fuego (40 s).
- **Manzana dorada**: 4 de absorción y regeneración II (5 s); la encantada,
  mucho más.
- **Pociones**: curación instantánea (4 de vida, 8 la II); arrojadizas,
  instantáneas a tus pies; de beber, 1.6 s quieto.
- Regeneración natural con el hambre a 18 o más.

### Otros
- **Caída**: un punto de daño por bloque pasados 3. El agua anula la caída: el
  **cubo de agua** echado justo antes del suelo ("MLG") la deja en 0.
- **Perla de ender**: unos 20-30 bloques en 1 s; 5 de daño al llegar.
- **Hacha contra escudo**: ver arriba. **Lava y fuego**: ver `safety`.

## 2. Lo que hace el bot

### Preparación
- **Mano izquierda** (`offhand.go`):
  - Fuera de pelea, el tótem (lo inesperado: una caída, la lava).
  - Con un jugador armado a menos de 16 bloques, el escudo, ya antes de que venga.
  - Peleando, el escudo; con un creeper cerca, siempre el escudo (para la explosión).
  - Con menos de 6 de vida, o rodeado y con menos de 8, el tótem.
  - El cambio se hace con la F (vale corriendo). Fuera de pelea deja el otro en
    la barra rápida. Si la F no surte efecto, al segundo intento usa un clic,
    quieto. Si un rival que pega de cerca ya está a menos de 7 bloques, no
    cambia: el cambio reinicia el golpe y no da tiempo a subir el escudo.
- **En guardia** (`beWary`, modo `wary`): un jugador armado que viene corriendo
  de cara a menos de 8 bloques. El bot se pone el arma en la mano y el escudo, le
  mira y sube el escudo con margen (a 7 bloques). No ataca, porque puede ser
  alguien de paso, pero **su primer golpe da en el escudo**, y ese golpe (evento
  29) empieza la pelea. Los dueños (`remoteOwners`) nunca.

### El duelo cuerpo a cuerpo (`thinkFight`)
- **Puntería y alcance con la posición prevista** del rival (su velocidad × 3
  ticks): lo que se ve llega tarde y el servidor mide con la suya.
- **Distancia**:
  - Con el golpe cargado, a por él.
  - Mientras recarga: con escudo, quieto tras él; sin escudo, fuera de su alcance
    (retrocediendo).
  - Con escudo, a uno que viene **no se le sale al paso**: se le espera y se le pega
    cuando entra a tiro.
- **Ritmo del escudo y ventana de castigo**: con escudo, se le espera tapado y
  se le pega en los 6 ticks siguientes a **su** golpe (su brazo, el golpe en el
  bot o en el escudo): tras pegar le faltan ~13 ticks para el siguiente, y da
  tiempo a volver a subir el escudo (5). Si en 25 ticks no pega (escudo contra
  escudo), se le pega igual. Contra un hacha no se levanta, y con el escudo
  desactivado tampoco.
- **Crítico**: se salta unos 7 ticks antes de que el golpe esté cargado y el
  rival a tiro, para ir cayendo justo entonces. Con él ya a tiro y el golpe
  listo en el suelo, se pega ya (esperar a caer es comerse otro golpe). En el
  aire, sin sprint.
- **Jump reset**: saltar esprintando hacia el rival al recibir el golpe se come
  parte del empuje. Con el retraso del servidor el aviso llega tarde, así que
  solo se hace si ese salto sirve además para el crítico.
- **Con quién**: el que antes muere, contando lo que cuesta llegar. Cambiar de
  arma cuenta como una recarga entera.
- **Barrido**: con espada y dos o más pegados al objetivo, el golpe va en el
  suelo y sin sprint (barre: les da a todos y los aparta) en vez del crítico.
- **Bordes**: con un precipicio en la dirección del empujón (a su espalda, o
  a ±30°, a menos de 3,5 bloques: lo que lleva un golpe con sprint recibido en
  el aire; `Walker.Safe` solo mira 2), ni
  retroceder ni saltar (en el aire el empujón lleva más lejos): de lado y hacia
  él, y el escudo arriba aunque no le mire. Con el precipicio detrás del rival,
  golpe con sprint (el que más empuja). Peleando con un jugador, el pathfinding
  no pone bloques (un puente al borde, con sus empujones, es caerse).
- **Arco y ballesta** (`bow.go`): de 9 a 40 bloques, si no va a llegar antes
  de tensar. Tensado entero (20 ticks), quieto, con la trayectoria de verdad
  (gravedad 0.05, rozamiento 0.99) y adelantado lo que dura el vuelo. La
  ballesta se carga (25 ticks) y dispara al momento.

### A la defensiva contra uno que le supera (`think.go`, `offhand.go`)
Con la postura `me supera` o `no le gano` y escudo:
- **Sin saltar** (ni crítico ni jump reset): en el aire el escudo está abajo.
  En los logs (full hierro contra full diamante filo V), los dos golpes de 6
  entraron con el bot saltando para el crítico.
- **Pegar solo si no le cuesta un golpe** (`safeOpening`): su golpe se ve
  `netLag` (4) ticks tarde, el del bot llega 4 después y el escudo vuelve a
  parar 5 ticks tras subirlo. Contra una espada (12.5 ticks entre golpes) no
  da tiempo nunca: cada "castigo" era un golpe de 6 recibido por 1.8 dado. Se
  pega con él de espaldas, comiendo, o contra armas lentas (hacha, tridente).
- **El escudo siempre en la mano izquierda**, aunque venga encima o quede poca
  vida (salvo que lleve hacha): el cambio de mano es un rato sin escudo.
- **Su arma, la más fuerte que se le ha visto en el último minuto**: sacar el
  puño o un cubo un momento no le hace inofensivo (en los logs la postura
  saltaba a `le gano` y el bot entraba).

Banco `TestOutclassedShield` (BENCH=1, 6 salidas de 6 a 11 bloques, 40 s;
bot full hierro, espada de hierro, escudo, tótem, zanahorias doradas, sin
bloques; rival full diamante prot IV, espada de diamante filo V):

| | Vivo | Golpes recibidos | Muertes |
|---|---:|---:|---:|
| Antes | 4.9 s | 5 (con el tótem gastado) | 6 de 6 |
| Ahora | 40 s | 0-2 (59-60 parados) | 0 de 6 |

No le mata: a la defensiva el bot no pierde, pero tampoco gana (le quita 1.8
por golpe y solo pega cuando no hay riesgo). Ganarle pide otra cosa (ver
Pendiente).

### Leer al rival (`matchup.go`)
Contra un jugador, antes de nada se mira su equipo (lo que lleva puesto y en
las manos se ve, con sus encantamientos) y el del bot, y se hace la cuenta del
intercambio de golpes:
- Lo que entra de un golpe: el arma y su **filo** (0.5 por nivel y 0.5) contra
  la **armadura** (puntos y dureza) y la **protección** del otro (4 % por nivel
  y pieza, hasta el 80 %). Aspecto ígneo suma algo.
- Golpes y segundos para matar a cada uno, con la vida, la absorción y lo que el
  bot tiene para curarse (tótem, doradas, pociones de curación) y el ritmo de
  cada arma. El escudo se come parte de los golpes del que no lleva hacha.

La relación (lo que tarda él en matar al bot / lo que tarda el bot) da la postura:

| Postura      | Cuándo   | Qué cambia                                                                 |
|--------------|----------|----------------------------------------------------------------------------|
| `le gano`    | ≥ 1.5    | no se retira hasta 5 de vida                                               |
| `igualado`   | 0.8–1.5  | lo de siempre                                                              |
| `me supera`  | 0.4–0.8  | se retira antes (+4), espera más lejos mientras recarga, escudo solo se baja en su ventana; para curarse, al búnker |
| `no le gano` | < 0.4    | en la zona no se le busca; si viene, perla para irse o **búnker** antes de que llegue; se retira antes (+6) |

Ejemplo (el del log): bot full hierro con espada de hierro contra full diamante
protección IV y espada de diamante filo V: el bot le quita 0.6 por golpe (36
golpes, 22 s) y él 6 (4 golpes, 2 s): `no le gano`.

### El búnker (`bunker.go`)
Contra un jugador, para curarse: correr no sirve y comer al descubierto es
comerse dos golpes. Con bloques (y el rival a 3 o más), se encierra en su celda.
Un jugador no pega a través de un bloque: tiene que romperlo.
- Al rival al que no se le gana se le ve venir **desde 16 bloques** (armado,
  de cara y acercándose): el búnker empieza antes de que llegue.
- Sin techo, saltando junto a la pared le ve por encima y le pega en la
  cabeza (medido: 3 golpes así).
- El techo necesita un bloque de apoyo encima de la pared del rival: su cara de
  arriba queda por encima de los ojos (de pie no se ve, y el anticheat no deja
  clicar una cara que no se ve), así que **salta** y lo pone en lo alto del
  salto. El techo, ya de pie (saltando, la cabeza ocupa su sitio). Un bloque
  cada 2 ticks.
- **Rápido**: cada bloque se da por puesto al mandarlo (como el cliente de
  vanilla, `Hands.PlaceOnNow`; si el servidor no lo pone, su respuesta lo
  quita y se repone), en vez de esperar la ida y vuelta del servidor por cada uno.
- Dentro come (dorada con 14 o menos, si no comida normal) y bebe curación,
  sin parar de vigilar: si le rompen la pared del lado del rival comiendo, deja
  de comer y la repone (`Hands.CancelEat`).
- **Orden**: la pared de su lado, el techo, la de atrás (la de su empujón: sin
  ella, un golpe por un lado le saca), y los dos lados. Techo a los 20 ticks,
  todo a los 32.
- **Ardiendo, primero apagarse**: con cubo de agua o agua a 8 bloques, no se
  encierra (dentro se sigue quemando: en los logs, de 14 a 9 encerrado
  ardiendo con agua al lado). Ir al agua va antes que el búnker, y también sin
  pelea (en guardia). Ya dentro, si se prende, el reflejo del cubo deja de
  comer para echarla (`TestFireBeforeBunker`, `TestFireInBunker`).
- Solo con **bloques para el búnker entero** (los que falten de paredes y
  techo, unos 10; antes bastaban 2 y se quedaba a medias) y con él a 5 o más
  si le mira (a 3-4 le saca antes de cerrar: en los logs, todos así acabaron
  en "me movieron").
- Se encierra antes de que llegue (lo ve venir desde 16) si no se le gana, o
  si con la vida de ahora le supera (con menos de 14).
- **El plan dentro** (lo dice en el log, `búnker, plan: …`):
  - Sin curar: comer (dorada con 14 o menos) y beber curación.
  - Curado y se le puede ganar: **sale por detrás** (por delante él espera
    pegado y pega primero, con el pico aún en la mano) y va a por él.
  - Curado y no se le gana, con perla: abre por detrás y se va con ella.
  - Curado, no se le gana, sin perla pero **con escudo** (y él sin hacha):
    sale, a la defensiva (antes esperaba dentro hasta 3 min;
    `TestBunkerLeaveWithShield`).
  - Curado, no se le gana y sin perla ni escudo: espera a que se vaya (3
    minutos como mucho; luego sale a pelear).
  - Sin comida (esperar no da vida): ya está curado lo que se puede.
  - El rival se va (a más de 24 o fuera de vista): sale por detrás.
- **Salir**: rompe pies y cabeza de un lado con la herramienta que vaya mejor
  (pico, pala o hacha; el bambú, con hacha; a mano, piedra son ~10 s por
  bloque, bambú 3 s), sin parar los pasos. Un bloque que no se rompe en 5
  intentos: otro lado.
- **Rescate**: si el búnker acaba sin abrir (le sacaron, sin bloques, una
  orden `stop`, reconectar) y está encerrado entre bloques que puso él, abre
  una salida solo (el pathfinding no rompe piedra). También en plena pelea:
  antes solo sin pelea, y con el rival cerca se quedaba dentro.
- Si el rival se mete en el hueco de la pared o le saca de la celda, a pelear
  (y no otro búnker en 5 s).

### Tácticas contra un jugador (`tactics.go`)
- **Tirarle de donde se subió**: 2 o más bloques por encima, sobre un solo
  bloque al alcance y sin estar metido en un pozo: le rompe ese bloque (con la
  herramienta que vaya). En una torre de 1×1 baja bloque a bloque hasta
  tenerle a tiro; con hueco debajo, cae y se hace daño.
- **Arco** (`bow.go`), además de lo de lejos: subido a algo y sin poder
  romperle el suelo (el empuje de la flecha le tira: medido, una flecha), o
  comiendo/bebiendo a 5 o más (que no se cure tranquilo; en Java el daño no
  corta el comer, pero cada flecha le quita lo que se cura). No dispara con su
  escudo arriba de cara ni con un bloque en medio. También cuando no se le
  llega (3 s sin acercarse, a 5 o más y con línea de tiro: al otro lado de un
  hueco o de agua).
- **La flecha tiene que llegar** (`arrowPathClear`): antes de sacar el arco y
  antes de soltar se sigue su vuelo de verdad (en arco, con la puntería
  adelantada) bloque a bloque. Con un bloque en medio no se tira: ni desde
  su búnker contra su propia pared, ni a uno que se tapó con bloques encima
  o alrededor. Tensado y sin tiro, espera; si cambia de plan, deja de tensar
  sin disparar. Antes la regla de lejos no lo miraba y las otras miraban la
  recta (`TestBowBlocked`: 7 flechas a la pared antes, 0 ahora).
- El arco contra uno subido, solo a 5 o más en horizontal (más cerca baja de
  un salto, y tensando no hay ni escudo ni espada).
- **Subir a por él**: subido a algo, si tras 5 flechas sigue arriba (o desde
  abajo no hay línea de tiro) y hay bloques para la altura, deja el arco 30 s
  y sube con torre (el pathfinding). Antes, sin línea de tiro, la regla de
  "lejos" tiraba contra la torre.
- **Cubrirse de sus flechas** (`cover`): un jugador le apunta con arco o
  ballesta a 6 o más. Con escudo, el escudo (Think, también persiguiéndole).
  Sin escudo (o desactivado) y con bloques: dos bloques (pies y cabeza) hacia
  él y quieto detrás mientras tensa (3 s como mucho; otra vez no antes de 6 s).
- **Su agua y su lava**: una fuente suelta (de un cubo, no un lago) a 3 o menos
  de cualquiera de los dos y al alcance: la recoge con un cubo vacío (se queda
  el cubo lleno), o la tapa con un bloque. La lava, antes. Entre dos intentos,
  1 s (`fluidWait`): la respuesta del servidor tarda ~8 ticks, y antes de que
  llegue el cubo aún se ve vacío; "recogerla" con el cubo ya lleno era echarla
  otra vez (en los logs: recoger, echar, recoger… 6 s, el agua esparcida). La
  misma fuente, 3 intentos como mucho. Se apunta a la superficie del agua
  (y+0.85): desde un lado, el centro lo tapa el bloque de al lado.
- **Encerrarle**: comiendo, bebiendo, quieto medio segundo o **arrinconado**
  (ya con dos lados tapados a la altura de los pies), a 5 o menos: le
  pone bloques detrás y a los lados (pies y cabeza); el lado del bot, abierto
  (se fija al empezar). Primero la de detrás (sin ella, el primer golpe le
  saca): hasta tenerla al alcance se acerca **sin pegarle**. A uno que muere
  de 2 golpes no se le encierra: se le remata.
- **Romperle el búnker**: encerrado a su altura (pies y cabeza tapados a los
  cuatro lados): le rompe la pared de su lado, la cabeza y luego los pies, con
  la herramienta que vaya; si la repone, otra vez. A uno **armado**, desde
  fuera de su alcance (a 3,9: su golpe llega a ~3,4 de centro a centro, el pico
  a 4,5): que salga a campo abierto, donde el escudo manda. Pelear en la
  puerta no: con la pared detrás no hay empujón y él pega en cuanto ve al bot
  (medido: 0 de 8 así, 8 de 8 desde fuera). En su búnker roto o en la puerta
  se le pega aunque el pathfinding no vea línea recta hasta él.
- **Apagarse** (`safety/fire.go`): ardiendo, cubo de agua a los pies y
  recogerla; va antes que la pelea (aspecto ígneo, lava). Mientras, la pelea
  no toca la mano (`Fighter.Reflex`): antes volvía a poner la espada a cada
  paso y el cubo no salía nunca (en el servidor, muerto quemado peleando).
  Baja el escudo antes de echarla (`ReleaseHands`), trae el cubo del
  inventario a la barra si no está en ella, se queda en el agua (o vuelve a
  ella si le empujan) hasta que el servidor dice que se apagó, y si le alejan
  se acerca para recogerla. Solo recoge fuentes (el agua que corre no se
  recoge), reintenta pasados 25 ticks (la ida y vuelta), y no la da por
  perdida hasta 10 s. **Rellenar**: fuera de pelea, con un cubo vacío en la
  barra, sin cubo lleno y agua a 4 bloques: la recoge (`TestRefillBucket`).
  La caída (`fall.go`), igual: reintenta y hasta 5 s.

### Decidir: pelear o irse
- Contra mobs: con poca vida se retira a comer (corriendo se les deja atrás).
  **Sin sitio al que huir** (un callejón, una cueva) y con bloques, **torre de
  3** (`pillar.go`): un zombi que salta contra ella llega a 2, no a 3. Arriba
  come hasta 18 y baja a pelear (de 3 bloques la caída no hace daño). En campo
  abierto no: medido, con torre es peor (al bajar se está rodeado).
- **Contra un jugador no se huye corriendo** (corre igual: es que te peguen por
  la espalda). Se sigue si **se gana el intercambio de golpes** (`trade`: los
  golpes que le hacen falta a cada uno con su arma y la armadura del otro) o si
  no hay con qué irse. Con una **perla**, perdiendo, se va lejos de él (con 7 o
  más de vida).
- Inalcanzable 8 s (detrás de un muro, en una torre): se deja un minuto.
- Muerto (vida 0, aunque su cuerpo siga ahí): se acabó.

### Recursos (`potions.go`)
- **Curarse**: con 8 o menos, arrojadiza de curación a los pies; si no hay, de
  beber con nadie a menos de 4.
- **Prepararse contra un jugador**: fuerza, velocidad y (si falta vida)
  regeneración. Arrojadiza a 3 bloques o más; de beber a 10 o más.
- **Manzana dorada** con 12 o menos y sin regeneración, sin nadie encima.

### Reflejos (`safety`)
- **Water drop** (`safety/fall.go`): con una caída que quite vida (un corazón
  ya cuenta: 4 bloques o más), el cubo a la mano (de la barra, o del
  inventario a la barra cayendo). **Dónde va a caer** se calcula con su
  velocidad (gravedad 0.08, rozamiento 0.98 en vertical y 0.91 de lado): con
  un empujón o saliendo corriendo de un borde cae en otra columna, y el agua
  va ahí, mirando a ese bloque. La echa cuando el tick siguiente está en
  alcance; cayendo a casi 4 bloques por tick, en la última ocasión antes del
  suelo. Luego **recoge el agua**. No hace nada si cae en agua, heno, slime,
  telaraña o nieve polvo. `TestWaterDrop`: de 4 a 100 bloques, con el cubo en
  el inventario y con empujones de lado (cae a 5-9 bloques): 0 de daño y el
  cubo lleno al final; sin cubo, las mismas caídas quitan 2 y 8.
  En una zona protegida (el spawn) el servidor no deja poner el agua.
- **Apagarse** (`safety/fire.go`): ardiendo (el bit 0x01 de los datos de su
  entidad), sin resistencia al fuego y en tierra, agua a los pies (se apaga al
  momento) y la recoge.
- **Lava y fuego**: ver `safety/lava.go`.

- **No ahogarse** (`safety/fire.go`): el aire que le queda llega en los datos
  de su entidad (`EntityAirSupply`, 300 lleno). Bajo el agua con menos de 150,
  deja la pelea y nada hacia arriba hasta tener el aire lleno (en los logs:
  ahogado peleando en un lago). `TestSurfaceToBreathe` (el simulador no
  ahoga: se le manda el aire a mano).

### Creepers y tiradores
- **Creeper**: se vigila siempre a menos de 8 (aunque haya poca vida). Encendido,
  se le planta cara con el escudo arriba y la explosión no hace nada. Sin escudo,
  se huye.
- **Esqueleto** (y otros tiradores): se va a por él en cuanto apunta. El escudo
  arriba mientras tensa y 15 ticks después de disparar, que es lo que tarda la
  flecha.

## 3. Medidas (simulador, 4 ticks de retraso)

Duelista: corre a 0.28 bloques/tick y pega con espada de hierro (6) en cuanto
tiene el golpe cargado, con empuje de sprint. Sin armadura ninguno de los dos.
12 salidas por caso, de 6 a 10 bloques.
`BENCH=1 go test -run TestDuelBench -v ./combat`.

| Equipo del bot              | Al empezar | Va a por él (`/fight`)  | Viene a por el bot     |
|-----------------------------|-----------:|------------------------:|-----------------------:|
| espada + escudo + tótem     | 0 de 12    | **12 de 12** (vida 12.6) | **8 de 12** (vida 8.8) |
| espada (con o sin comida)   | 0 de 12    | 0 de 12                 | 0 de 12                |

| Situación                                      | Resultado                                              |
|------------------------------------------------|--------------------------------------------------------|
| Plataforma de 9×9 sobre el vacío, con escudo   | 0 caídas                                               |
| La misma, sin escudo                           | 0 de 160 con el margen de 3,5 bloques (6 de 16 mirando 2; 13 de 16 sin lógica de bordes) |
| Caída de 10, 25 y 40 bloques con cubo de agua  | 0 de daño, recoge el agua (sin cubo: de 20 a 3 en la de 25) |
| Ardiendo 8 s con cubo de agua                  | 0 de daño, recoge el agua                              |
| Ardiendo peleando, cubo en la barra o en el inventario (`TestFireInFight`) | se apaga 40 de 40; recoge el agua 39 de 40 |
| Zombi que viene de 20 bloques, con arco        | 4 flechas, 4 aciertos: muere sin llegar                 |
| 4 zombis, espada de hierro                     | 13.3 s y vida mínima 14 con barrido; 34.1 s y 7 sin él |

- **Con escudo**, en la emboscada las derrotas que quedan son las salidas a 6
  bloques: el rival aparece encima y no da tiempo a cambiar al escudo. Visto
  venir de lejos (20 bloques) gana sin gastar el tótem (`TestDuel`).
- **Sin escudo, contra un igual** que pega primero, el simulador da la victoria
  al que empieza casi siempre: tras cada intercambio los dos salen despedidos y
  encadenar críticos exige ser el que empuja. No se ha forzado ese caso (sería
  ajustar para el test).

Leer al rival y el búnker (bot full hierro, espada de hierro; rival full
diamante protección IV, espada de diamante filo V, `TestMatchupOutclassed`):

| Caso                                                | Resultado                                    |
|-----------------------------------------------------|----------------------------------------------|
| Cerrarse del todo (`TestBunkerSpeed`)               | techo a los 20 ticks, 8 paredes a los 32 (al principio: paredes a los 53, sin techo) |
| Con bloques, rival sin pico                         | se encierra al verle venir: 0 golpes en 60 s |
| Igual, el bot con 8 de vida                         | se cura dentro hasta 20, 0 golpes            |
| Rival con pico (un bloque cada 2 s), bot con 8      | 13.8 s vivo (repone 5 bloques rotos, pero entra) |
| El rival se va (`TestBunkerExit`)                   | sale por detrás: 2 s con pico, 20 s a mano   |
| Igualado (full hierro), bot con 6, sin escudo       | se encierra, se cura a 19, sale y pelea (y pierde: el duelo sin escudo) |
| Igual con escudo                                    | la cuenta sale igualada: pelea sin búnker y gana |
| Orden `stop` estando cerrado (`TestBunkerUnbox`)    | el rescate abre una salida                   |
| Sin bloques, bot con 8                              | 2.3 s vivo                                   |

Tácticas (`tactics_test.go`, rival quieto o comiendo):

| Caso                                              | Resultado                                     |
|---------------------------------------------------|-----------------------------------------------|
| En una torre de 1×1 de 5, a 8 bloques, con arco   | una flecha: le tira (de 69 a 66.9)            |
| En una torre de 5×5 de 5, con arco y bloques      | flechas, y sube con torre hasta él            |
| En una torre de 1×1 de 3, al lado, con pico       | le rompe el suelo hasta bajarle               |
| Pone agua / lava a su lado                        | la recoge con el cubo; sin cubo, la tapa      |
| Comiendo, a 4                                     | 6 bloques (detrás y lados), su lado abierto   |
| Comiendo, a 6, con arco                           | flechas                                       |
| Matarle encerrado / sin encerrar (medida)         | 16.9 s / 15.5 s: aquí cuesta más (ver abajo)  |
| En su búnker (piedra, con techo), comiendo        | le rompe la pared y le mata: 8 de 8           |
| En su búnker, armado (bot con escudo)             | se la rompe desde fuera, sale y le mata: 8 de 8 |

La jaula cuesta tiempo (6 bloques y la espada otra vez en la mano: el golpe sin
cargar). Su ventaja, que no se vaya y que el empujón no le aleje, es contra uno
que se mueve; el rival del simulador comiendo no se mueve aunque le empujen.

El simulador ya modela armadura, protección, filo, línea de visión para pegar,
un rival que rompe bloques, quieto o comiendo (`simworld.Kit`), el empuje de
las flechas, recoger lava con el cubo y poner bloques en el agua.

## 4. Pendiente

- **Probar en el servidor real**. El simulador no tiene pociones,
  ni perlas.
- Cubo de agua para frenar a quien persigue, y cubo de lava contra él.
- Probar en el banco "pegar si está al alcance aunque no haya línea recta"
  como regla general: la emboscada pasó de 8 a 12 de 12, pero otros casos
  empeoraron (el escudo se subía tarde); sin estudiar.
- Búnker contra uno con pico: lo rompe y entra (medido abajo). Pelear por la
  abertura en vez de salir.
- Telarañas, caña de pescar, encantamientos (aspecto ígneo, retroceso).
- **Ganarle a uno que le supera** (hoy solo no pierde): el entorno (su lava,
  tirarle por un borde, la caída), cubo de lava contra él, hacha contra su
  escudo, pociones arrojadizas de daño/veneno/debilidad, perlas para cortar
  la distancia o irse, telarañas, caña de pescar para cortar sus combos.
- Ardiendo sin cubo: va al agua que haya a 8 bloques o menos (`fire.go`,
  `TestFireToWater` 8 de 8). Sin agua cerca, nada.
- `netLag` es fijo (4 ticks): medir el ping real.
- Cubrirse de las flechas: el simulador no tiene un rival con arco; sin probar.
- Recoger el agua: 1 de 40 la deja (la recogida de "su agua" de la pelea
  falla al recoger la del propio bot cuando el reflejo ya la dio por perdida).
