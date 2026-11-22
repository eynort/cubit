package hub

// El adaptador real de dcSession sobre github.com/bwmarrin/discordgo.
// discordgo ya reconecta solo cuando se cae una conexión abierta; el gestor
// (discord.go) se ocupa de los reintentos del primer Open. Solo hace falta el
// intent Guilds: las órdenes con barra no leen mensajes.

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type goSession struct {
	s *discordgo.Session
}

func newGoSession(token string) dcSession {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return failedSession{err}
	}
	s.Identify.Intents = discordgo.IntentsGuilds
	s.ShouldReconnectOnError = true
	return &goSession{s: s}
}

// failedSession: no se pudo ni crear la sesión.
type failedSession struct{ err error }

func (f failedSession) Open(func(bool), func(dcInteraction)) error { return f.err }
func (f failedSession) RegisterCommands(string) error              { return f.err }
func (f failedSession) Send(string, string) error                  { return f.err }
func (f failedSession) Close() error                               { return nil }

func (g *goSession) Open(onState func(bool), onCmd func(dcInteraction)) error {
	g.s.AddHandler(func(*discordgo.Session, *discordgo.Ready) { onState(true) })
	g.s.AddHandler(func(*discordgo.Session, *discordgo.Resumed) { onState(true) })
	g.s.AddHandler(func(*discordgo.Session, *discordgo.Disconnect) { onState(false) })
	g.s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		d := i.ApplicationCommandData()
		in := dcInteraction{Command: d.Name, Options: map[string]string{}}
		if i.Member != nil && i.Member.User != nil {
			in.UserID = i.Member.User.ID
		} else if i.User != nil {
			in.UserID = i.User.ID
		}
		for _, o := range d.Options {
			in.Options[o.Name] = o.StringValue()
		}
		in.Reply = func(text string, ephemeral bool) error {
			text = cut(text, 1900)
			data := &discordgo.InteractionResponseData{Content: text, AllowedMentions: &discordgo.MessageAllowedMentions{}}
			if ephemeral {
				data.Flags = discordgo.MessageFlagsEphemeral
			}
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: data})
		}
		onCmd(in)
	})
	return g.s.Open()
}

func (g *goSession) RegisterCommands(channelID string) error {
	if g.s.State == nil || g.s.State.User == nil {
		return fmt.Errorf("no application id yet")
	}
	str := func(n, d string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: n, Description: d, Required: true}
	}
	cmds := []*discordgo.ApplicationCommand{
		{Name: "bot", Description: "Run a Cubit command on a bot", Options: []*discordgo.ApplicationCommandOption{str("name", "Which bot"), str("command", "The command line, e.g. /goto 100 64 200")}},
		{Name: "bots", Description: "List the bots and what they're up to"},
		{Name: "status", Description: "How one bot is doing", Options: []*discordgo.ApplicationCommandOption{str("name", "Which bot")}},
	}
	guild := ""
	if channelID != "" {
		if ch, err := g.s.Channel(channelID); err == nil {
			guild = ch.GuildID // en el servidor del canal salen al momento
		}
	}
	_, err := g.s.ApplicationCommandBulkOverwrite(g.s.State.User.ID, guild, cmds)
	return err
}

func (g *goSession) Send(channelID, text string) error {
	_, err := g.s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: text, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	return err
}

func (g *goSession) Close() error { return g.s.Close() }
