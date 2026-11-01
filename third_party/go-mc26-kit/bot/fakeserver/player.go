package fakeserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26-kit/server"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	mcnet "github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Player is a client in play on the server: who it is, its connection, what it sent. The single-player API
// of Server (Send, Got, WaitFor, Said, Chat, Kick, Joined) is about the first player; Players and Player
// give every one (MULTIPLAYER.md).
type Player struct {
	Name     string
	UUID     uuid.UUID // the offline uuid of the name
	EntityID int32     // 1, 2, 3 … in the order the players joined
	// Hook, when set, is called with every packet this player sends, after the server's Hook.
	Hook func(Packet)

	mu     sync.Mutex
	conn   *mcnet.Conn
	lag    int          // ticks the packets to the player are held (SetLag), 0 none
	held   []heldPacket // packets on their way (Lagged)
	got    []Packet
	listed map[string]uuid.UUID // the players Chat has put in this player's list
	pos    [3]float64           // where it is (Spawn, or where Teleport put it)
	tpID   int32                // the last teleport id told
	chunks *chunkSender         // its chunks (chunks.go)
	news   chan struct{}        // a packet came, or the player left
	joined chan struct{}
	left   chan struct{}
}

func newPlayer() *Player {
	return &Player{news: make(chan struct{}, 1), joined: make(chan struct{}), left: make(chan struct{}), pos: Spawn, tpID: 1, chunks: newChunkSender()}
}

// the is the player the single-player API is about: the first in play, or before any, the one the first
// client to join will be (a WaitFor before the join waits on it).
func (s *Server) the() *Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.players) > 0 {
		return s.players[0]
	}
	return s.first
}

// maxPlayers is MaxPlayers, one when unset.
func (s *Server) maxPlayers() int { return max(s.MaxPlayers, 1) }

// multi reports whether the server tells its players of each other: it takes more than one.
func (s *Server) multi() bool { return s.maxPlayers() > 1 }

// Players returns the players in play, in the order they joined.
func (s *Server) Players() []*Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.players)
}

// Player returns the player in play of the name, or nil.
func (s *Server) Player(name string) *Player {
	for _, p := range s.Players() {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// WaitPlayers waits till n players are in play (Joined, for more than one).
func (s *Server) WaitPlayers(n int, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		ps := s.Players()
		if len(ps) >= n {
			for _, p := range ps {
				select {
				case <-p.joined: // in play: told its level and the others
				case <-p.left:
					return fmt.Errorf("fakeserver: %s left before it was in play", p.Name)
				case <-deadline:
					return fmt.Errorf("fakeserver: %s not in play", p.Name)
				}
			}
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("fakeserver: %d of %d players joined", len(ps), n)
		case <-time.After(time.Millisecond):
		}
	}
}

// joinedAs waits for the player of the name to be in play.
func (s *Server) joinedAs(name string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		if p := s.Player(name); p != nil {
			select {
			case <-p.joined:
				return nil
			case <-deadline:
			}
		} else {
			select {
			case <-deadline:
			case <-time.After(time.Millisecond):
				continue
			}
		}
		return fmt.Errorf("fakeserver: %s did not join", name)
	}
}

// Broadcast sends a packet to every player in play.
func (s *Server) Broadcast(p Clientbound) error {
	ps := s.Players()
	if len(ps) == 0 {
		return errors.New("fakeserver: no bot")
	}
	var errs []error
	for _, pl := range ps {
		errs = append(errs, pl.Send(p))
	}
	return errors.Join(errs...)
}

// roster is the server's list of players as the list ping and the login see it: MaxPlayers and the players
// in play. A login past MaxPlayers is refused with the vanilla reason.
type roster Server

func (r *roster) MaxPlayer() int    { return (*Server)(r).maxPlayers() }
func (r *roster) OnlinePlayer() int { return len((*Server)(r).Players()) }

func (r *roster) PlayerSamples() []server.PlayerSample {
	var out []server.PlayerSample
	for _, p := range (*Server)(r).Players() {
		out = append(out, server.PlayerSample{Name: p.Name, ID: p.UUID})
	}
	return out
}

func (r *roster) CheckPlayer(string, uuid.UUID, int32) (bool, chat.Message) {
	if r.OnlinePlayer() >= r.MaxPlayer() {
		return false, chat.TranslateMsg("multiplayer.disconnect.server_full")
	}
	return true, chat.Message{}
}

// admit makes the player of a connection that reached play, or refuses it when the server is full (two logins
// at once can both pass CheckPlayer). The first player is the one made with the server.
func (s *Server) admit(name string, id uuid.UUID, conn *mcnet.Conn) *Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.players) >= s.maxPlayers() {
		return nil
	}
	p := s.first
	if p.connected() || p.Name != "" { // the first player came, and maybe went: a new one
		p = newPlayer()
	}
	s.lastID++
	p.Name, p.UUID, p.EntityID = name, id, s.lastID
	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()
	s.players = append(s.players, p)
	return p
}

// leave takes the player out of play, and tells the others.
func (s *Server) leave(p *Player) {
	s.mu.Lock()
	s.players = slices.DeleteFunc(s.players, func(q *Player) bool { return q == p })
	others := slices.Clone(s.players)
	s.mu.Unlock()
	if s.multi() {
		for _, o := range others {
			_ = o.Send(play.PlayerInfoRemove{ProfileIds: types.List[pk.UUID, *pk.UUID]{pk.UUID(p.UUID)}})
			_ = o.Send(play.RemoveEntities{EntityIds: types.List[pk.VarInt, *pk.VarInt]{pk.VarInt(p.EntityID)}})
		}
	}
	close(p.left)
	p.signal()
}

// presence tells the player who joined of the players in play, and them of it: their entries in the player
// list (its own among them) and their entities at Spawn, as a vanilla server tells them.
func (s *Server) presence(p *Player) {
	all := s.Players()
	for _, o := range all {
		_ = p.Send(infoAdd(o))
		if o != p {
			_ = p.Send(spawnOf(o))
			_ = o.Send(infoAdd(p))
			_ = o.Send(spawnOf(p))
		}
	}
}

func infoAdd(p *Player) play.PlayerInfoUpdate {
	acts := make(types.EnumSet[types.PlayerInfoUpdateAction], (types.PlayerInfoUpdateActionAddPlayer.Count()+7)/8)
	acts.Set(types.PlayerInfoUpdateActionAddPlayer)
	return play.PlayerInfoUpdate{Actions: acts, Entries: []play.PlayerInfoUpdateEntry{{ProfileID: pk.UUID(p.UUID), Name: pk.String(p.Name)}}}
}

func spawnOf(p *Player) play.AddEntity {
	return play.AddEntity{ID: pk.VarInt(p.EntityID), UUID: pk.UUID(p.UUID), Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:player")),
		X: pk.Double(Spawn[0]), Y: pk.Double(Spawn[1]), Z: pk.Double(Spawn[2])}
}

// relayChat sends what a player said in chat to every player, itself too, as a server relays chat.
func (s *Server) relayChat(from *Player, got Packet) {
	var c play.Chat
	if got.Scan(&c) != nil {
		return
	}
	for _, o := range s.Players() {
		_ = o.Send(playerChat(from.Name, from.UUID, string(c.Message)))
	}
}

// playerChat is a player's message as a server relays it: unsigned, decorated "<from> text" by an inline
// chat type.
func playerChat(from string, id uuid.UUID, text string) play.PlayerChat {
	decoration := func(key string) chat.ChatTypeDecoration {
		return chat.ChatTypeDecoration{TranslationKey: key, Parameters: []chat.ChatTypeDecorationParameter{"sender", "content"}}
	}
	return play.PlayerChat{
		Sender: pk.UUID(id),
		Body:   types.SignedMessageBodyPacked{Content: pk.String(text)},
		ChatType: chat.Type{ID: -1, SenderName: chat.Text(from), // inline: the chat type is in the message
			Inline: &chat.InlineType{Chat: decoration("chat.type.text"), Narration: decoration("chat.type.text.narrate")}},
	}
}

func (p *Player) connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn != nil
}

func (p *Player) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

func (p *Player) signal() {
	select {
	case p.news <- struct{}{}:
	default:
	}
}

// Send sends the player a packet.
func (p *Player) Send(c Clientbound) error {
	return p.SendRaw(pk.Marshal(c.PacketID(), c))
}

// heldPacket is a packet on its way to the player: it leaves after ticks more ticks (Lagged).
type heldPacket struct {
	pkt   pk.Packet
	ticks int
}

// SetLag makes the packets to the player arrive ticks later (a connection's lag, one way): each is held
// till LagTick has been called that many times, in the order sent. 0 sends them at once again (and what
// was held goes now).
func (p *Player) SetLag(ticks int) error {
	p.mu.Lock()
	p.lag = max(0, ticks)
	p.mu.Unlock()
	if ticks <= 0 {
		return p.release(true)
	}
	return nil
}

// LagTick is a tick of the lag: the held packets come a tick nearer, and those due are sent.
func (p *Player) LagTick() error { return p.release(false) }

func (p *Player) release(all bool) error {
	p.mu.Lock()
	conn := p.conn
	n := 0
	for i := range p.held {
		if p.held[i].ticks--; all || p.held[i].ticks <= 0 && n == i {
			n = i + 1 // (in order: one held back holds back those after it)
		}
	}
	due := append([]heldPacket(nil), p.held[:n]...)
	p.held = p.held[n:]
	p.mu.Unlock()
	if conn == nil {
		return nil
	}
	for _, h := range due {
		if err := conn.WritePacket(h.pkt); err != nil {
			return err
		}
	}
	return nil
}

// SendNow sends the player a packet past the lag (a harness's own packet, as a sync's ping: what the
// bot has received by now, not what is on its way).
func (p *Player) SendNow(c Clientbound) error {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return errors.New("fakeserver: no bot")
	}
	return conn.WritePacket(pk.Marshal(c.PacketID(), c))
}

// SendRaw sends the player a packet already made.
func (p *Player) SendRaw(pkt pk.Packet) error {
	p.mu.Lock()
	if p.lag > 0 {
		p.held = append(p.held, heldPacket{pkt: pkt, ticks: p.lag})
		p.mu.Unlock()
		return nil
	}
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return errors.New("fakeserver: no bot")
	}
	return conn.WritePacket(pkt)
}

// Got is every packet of the kind the player sent so far.
func (p *Player) Got(id packetid.ServerboundPacketID) []Packet {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Packet
	for _, g := range p.got {
		if g.ID == id {
			out = append(out, g)
		}
	}
	return out
}

// WaitFor waits for the n-th packet (from 1) of the kind the player sends.
func (p *Player) WaitFor(ctx context.Context, id packetid.ServerboundPacketID, n int) (Packet, error) {
	for {
		if got := p.Got(id); len(got) >= n {
			return got[n-1], nil
		}
		select {
		case <-ctx.Done():
			return Packet{}, fmt.Errorf("fakeserver: %d %v not sent (%d were): %w", n, id, len(p.Got(id)), ctx.Err())
		case <-p.left:
			return Packet{}, fmt.Errorf("fakeserver: the bot left before %d %v", n, id)
		case <-p.news:
		}
	}
}
