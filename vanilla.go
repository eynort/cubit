package main

// vanillaCommands: los comandos de un servidor vanilla (26.2) que un jugador
// puede llegar a usar. El bot solo usa estos: nada de comandos de plugins
// (/tpa, /home, /spawn, /login…), aunque el servidor los tenga.
var vanillaCommands = map[string]bool{
	"advancement": true, "attribute": true, "ban": true, "ban-ip": true, "banlist": true, "bossbar": true,
	"clear": true, "clone": true, "damage": true, "data": true, "datapack": true, "debug": true,
	"defaultgamemode": true, "deop": true, "difficulty": true, "effect": true, "enchant": true, "execute": true,
	"experience": true, "fill": true, "fillbiome": true, "forceload": true, "function": true, "gamemode": true,
	"gamerule": true, "give": true, "help": true, "item": true, "jfr": true, "kick": true, "kill": true,
	"list": true, "locate": true, "loot": true, "me": true, "msg": true, "op": true, "pardon": true,
	"pardon-ip": true, "particle": true, "perf": true, "place": true, "playsound": true, "random": true,
	"recipe": true, "reload": true, "return": true, "ride": true, "rotate": true, "save-all": true,
	"save-off": true, "save-on": true, "say": true, "schedule": true, "scoreboard": true, "seed": true,
	"setblock": true, "setidletimeout": true, "setworldspawn": true, "spawnpoint": true, "spectate": true,
	"spreadplayers": true, "stop": true, "stopsound": true, "summon": true, "tag": true, "team": true,
	"teammsg": true, "teleport": true, "tell": true, "tellraw": true, "tick": true, "time": true,
	"title": true, "tm": true, "tp": true, "transfer": true, "trigger": true, "w": true, "weather": true,
	"whitelist": true, "worldborder": true, "xp": true,
}
