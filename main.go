package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "github.com/joho/godotenv/autoload"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type PinnedMessage struct {
	TargetChannelID string
	TargetID        string
	MirrorChannelID string
	MirrorID        string
}
type Guild struct {
	ID             string          `bson:"id", omitempty`
	BoardChannelID string          `bson:"board_channel_id", omitempty`
	WatchChannels  []string        `bson:"watch_channels", omitempty`
	PinnedMessages []PinnedMessage `bson:"pinned_messages", omitempty`
}

// Variables used for command line parameters
var (
	dbClient *mongo.Client
	guildsDb *mongo.Collection
	dbURL    string
	dbName   string
)

func main() {
	var Token string
	if v, ok := os.LookupEnv("DISCORD_TOKEN"); ok {
		Token = v
	} else {
		log.Fatal("Please set DISCORD_TOKEN")
	}
	if v, ok := os.LookupEnv("MONGO_URI"); ok {
		dbURL = v
	} else {
		log.Fatal("Please set MONGO_URI")
	}
	if v, ok := os.LookupEnv("DBNAME"); ok {
		dbName = v
	} else {
		log.Fatal("Please set DBNAME")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var err error
	dbClient, err = mongo.Connect(ctx, options.Client().ApplyURI(
		dbURL,
	))
	if err != nil {
		log.Fatal(err)
	}
	guildsDb = dbClient.Database(dbName).Collection("guilds")
	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}
	defer dg.Close()
	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)
	dg.AddHandler(channelPinHandler)
	// In this example, we only care about receiving message events.
	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsDirectMessages | discordgo.IntentsGuilds)

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}
	dg.UpdateStatus(0, "with Marco • s!help")
	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {

	// Ignore all messages created by the bot itself
	// This isn't required in this specific example but it's a good practice.
	if m.Author.ID == s.State.User.ID {
		return
	}
	if m.Content == "s!setup" {
		var g Guild
		err := guildsDb.FindOne(context.Background(), bson.D{{Key: "id", Value: m.GuildID}}).Decode(&g)
		g.BoardChannelID = m.ChannelID
		g.ID = m.GuildID
		if err != nil {
			if err == mongo.ErrNoDocuments {
				_, err = guildsDb.InsertOne(context.Background(), g)
				if err != nil {
					log.Println(err)
				} else {
					s.ChannelMessageSend(m.ChannelID, "This channel has been chosen as the Starboard.\nPlease run s!watch on channels that you want to monitor pins.")
				}
			}
			return
		}
		g.BoardChannelID = m.ChannelID

		guildsDb.UpdateOne(context.Background(), bson.D{{Key: "id", Value: m.GuildID}}, bson.D{{"$set", bson.D{{Key: "board_channel_id", Value: m.ChannelID}}}})
		s.ChannelMessageSend(m.ChannelID, "This channel has been chosen as the Starboard.")
		return
	}
	if m.Content == "s!migrate" {
		var g Guild
		err := guildsDb.FindOne(context.Background(), bson.D{{"id", m.GuildID}}).Decode(&g)
		if err != nil {
			log.Println(err)
			s.ChannelMessageSend(m.ChannelID, "Please run s!setup on this guild.")
			return
		}
		channelPin(s, &discordgo.ChannelPinsUpdate{ChannelID: m.ChannelID, GuildID: m.GuildID})
		s.ChannelMessageSend(m.ChannelID, "Migrated this channel's pinned messages to Starboard successfully")
		return
	}
	if m.Content == "s!watch" {
		var g Guild
		err := guildsDb.FindOne(context.Background(), bson.D{{"id", m.GuildID}}).Decode(&g)
		if err != nil {
			log.Println(err)
			s.ChannelMessageSend(m.ChannelID, "Please run s!setup on this guild.")
			return
		}
		alreadyWatched := false
		for _, c := range g.WatchChannels {
			if c == m.ChannelID {
				alreadyWatched = true
				break
			}
		}
		if alreadyWatched {
			s.ChannelMessageSend(m.ChannelID, "I've already been watching this channel")
		} else {
			g.WatchChannels = append(g.WatchChannels, m.ChannelID)
			_, err := guildsDb.UpdateOne(context.Background(), bson.D{{Key: "id", Value: m.GuildID}}, bson.D{{"$set", bson.D{{Key: "watch_channels", Value: g.WatchChannels}}}})
			if err != nil {
				log.Println(err)
			} else {
				s.ChannelMessageSend(m.ChannelID, "I will be watching this channel from now.")
			}
		}
		return
	}
	if m.Content == "s!unwatch" {
		var g Guild
		err := guildsDb.FindOne(context.Background(), bson.D{{"id", m.GuildID}}).Decode(&g)
		if err != nil {
			log.Println(err)
			s.ChannelMessageSend(m.ChannelID, "Please run s!setup on this guild.")
			return
		}
		alreadyWatched := false
		idx := -1
		for i, c := range g.WatchChannels {
			if c == m.ChannelID {
				alreadyWatched = true
				idx = i
				break
			}
		}
		if !alreadyWatched {
			s.ChannelMessageSend(m.ChannelID, "I've never been watching this channel")
		} else {
			g.WatchChannels = append(g.WatchChannels[:idx-1], g.WatchChannels[idx+1:]...)
			_, err := guildsDb.UpdateOne(context.Background(), bson.D{{Key: "id", Value: m.GuildID}}, bson.D{{"$set", bson.D{{Key: "watch_channels", Value: g.WatchChannels}}}})
			if err != nil {
				log.Println(err)
			} else {
				s.ChannelMessageSend(m.ChannelID, "I will stop watching this channel from now.")
			}
		}
		return
	}
	if m.Content == "s!help" {
		e := &discordgo.MessageEmbed{
			Author:      &discordgo.MessageEmbedAuthor{Name: fmt.Sprintf("Hi %s, I'm %s", m.Author.Username, s.State.User.Username)},
			Description: "I will watch your pins and mirror it to the starboard.",
			Fields: []*discordgo.MessageEmbedField{
				{Name: "s!help", Value: "Show this help"},
				{Name: "s!setup", Value: "Make the current channel starboard"},
				{Name: "s!watch", Value: "Watch for pins in this channel"},
				{Name: "s!unwatch", Value: "Stop watching for pins in this channel"},
				{Name: "s!migrate", Value: "Mirror all pinned message in this channel to the starboard"},
			},
		}
		s.ChannelMessageSendEmbed(m.ChannelID, e)
		return
	}
}

func channelPinHandler(s *discordgo.Session, m *discordgo.ChannelPinsUpdate) {
	var g Guild
	err := guildsDb.FindOne(context.Background(), bson.D{{"id", m.GuildID}}).Decode(&g)
	if err != nil {
		log.Println(err)
		return
	}
	alreadyWatched := false
	for _, c := range g.WatchChannels {
		if c == m.ChannelID {
			alreadyWatched = true
			break
		}
	}
	if !alreadyWatched {
		return
	}
	channelPin(s, m)
}

func channelPin(s *discordgo.Session, m *discordgo.ChannelPinsUpdate) {
	messages, err := s.ChannelMessagesPinned(m.ChannelID)
	if err != nil {
		return
	}
	//TODO: context timeout
	var g Guild
	err = guildsDb.FindOne(context.Background(), bson.D{{"id", m.GuildID}}).Decode(&g)
	if err != nil {
		log.Println(err)
		return
	}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		e := &discordgo.MessageEmbed{}
		e.Author = &discordgo.MessageEmbedAuthor{Name: m.Author.Username, IconURL: m.Author.AvatarURL("")}
		e.Description = m.Content
		hasImage := false
		for _, a := range m.Attachments {
			if !hasImage && a.Height > 0 && a.Width > 0 {
				e.Image = &discordgo.MessageEmbedImage{URL: a.URL, ProxyURL: a.ProxyURL}
				hasImage = true
			} else {
				e.Description += fmt.Sprintf("\n\n[%s](%s)", a.Filename, a.URL)
			}
		}
		e.Description += fmt.Sprintf("\n\n[Link to message](https://discordapp.com/channels/%s/%s/%s)", g.ID, m.ChannelID, m.ID)
		t, _ := discordgo.SnowflakeTimestamp(m.ID)
		e.Footer = &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("MessageID: %s • %s", m.ID, t.Format("02/01/2006"))}

		alreadyMirrored := false
		for _, p := range g.PinnedMessages {
			if m.ChannelID == p.TargetChannelID && m.ID == p.TargetID {
				if _, err := s.ChannelMessage(p.MirrorChannelID, p.MirrorID); err == nil {
					alreadyMirrored = true
				}
				break
			}
		}
		if !alreadyMirrored {
			msg, err := s.ChannelMessageSendEmbed(g.BoardChannelID, e)
			if err == nil {
				g.PinnedMessages = append(g.PinnedMessages, PinnedMessage{
					TargetChannelID: m.ChannelID,
					TargetID:        m.ID,
					MirrorChannelID: msg.ChannelID,
					MirrorID:        msg.ID,
				})
				if dbName != "dev" {
					s.ChannelMessageUnpin(m.ChannelID, m.ID)
				}
			}
		}
	}
	guildsDb.UpdateOne(context.Background(), bson.D{{"id", m.GuildID}}, bson.D{{"$set", bson.D{{Key: "pinned_messages", Value: g.PinnedMessages}}}})
}
