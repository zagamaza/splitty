package bot

import (
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"log"
	"math/rand"
	"time"
)

// WTF bot bans user for random interval
type WTF struct {
	superUser   SuperUser
	minDuration time.Duration
	maxDuration time.Duration
	rand        func(n int64) int64
}

// NewWTF makes a random ban bot
func NewWTF(minDuration, maxDuration time.Duration, superUser SuperUser) *WTF {
	log.Printf("[INFO] WTF bot with %v-%v interval", minDuration, maxDuration)
	rand.Seed(time.Now().UnixNano())
	return &WTF{minDuration: minDuration, maxDuration: maxDuration, rand: rand.Int63n, superUser: superUser}
}

// OnMessage sets duration of ban randomly
func (w WTF) OnMessage(update api.Update) (response api.Response) {
	if update.Message == nil {
		return api.Response{}
	}

	if !contains(w.ReactOn(), update.Message.Text) {
		return api.Response{}
	}

	if w.superUser.IsSuper(update.Message.From.Username) {
		return api.Response{}
	}

	mention := "@" + update.Message.From.Username
	if update.Message.From.Username == "" {
		mention = update.Message.From.DisplayName
	}

	banDuration := w.minDuration + time.Second*time.Duration(w.rand(int64(w.maxDuration.Seconds()-w.minDuration.Seconds())))
	return api.Response{
		Text:        fmt.Sprintf("[%service](tg://user?id=%d) получает бан на %v", mention, update.Message.From.ID, HumanizeDuration(banDuration)),
		Send:        true,
		BanInterval: banDuration,
	}
}

// ReactOn keys
func (w WTF) ReactOn() []string {
	return []string{"wtf!", "wtf?"}
}

// Help returns help message
func (w WTF) Help() string {
	return genHelpMsg(w.ReactOn(), "если не повезет, блокирует пользователя на какое-то время")
}
