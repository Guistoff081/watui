package app

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/watui/watui/internal/theme"
)

// stickerMsgs builds n sticker messages (s0..s{n-1}) in ascending time order,
// all downloadable (DirectPath set, MediaPath empty).
func stickerMsgs(jid string, n int) []theme.Message {
	msgs := make([]theme.Message, n)
	for i := range msgs {
		msgs[i] = theme.Message{
			ID:         fmt.Sprintf("s%d", i),
			ChatJID:    jid,
			MediaType:  "sticker",
			DirectPath: "/direct/" + fmt.Sprint(i),
			Timestamp:  time.Unix(int64(100+i), 0),
		}
	}
	return msgs
}

func TestStickersToAutoDownloadNewestFirst(t *testing.T) {
	msgs := stickerMsgs("c@s.whatsapp.net", 5)
	got := msgIDs(stickersToAutoDownload(msgs, 3))
	want := []string{"s4", "s3", "s2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stickersToAutoDownload() = %v, want %v", got, want)
	}
}

func TestStickersToAutoDownloadSkipsIneligible(t *testing.T) {
	msgs := stickerMsgs("c@s.whatsapp.net", 5)
	msgs[4].MediaPath = "/cache/s4.webp" // already downloaded
	msgs[3].DirectPath = ""              // nothing to download from
	msgs[2].MediaType = "image"          // not a sticker
	got := msgIDs(stickersToAutoDownload(msgs, 10))
	want := []string{"s1", "s0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stickersToAutoDownload() = %v, want %v", got, want)
	}
}

func TestStickersToAutoDownloadEmpty(t *testing.T) {
	if got := stickersToAutoDownload(nil, 10); len(got) != 0 {
		t.Fatalf("stickersToAutoDownload(nil) = %v, want empty", msgIDs(got))
	}
	if got := stickersToAutoDownload(stickerMsgs("c@s.whatsapp.net", 3), 0); len(got) != 0 {
		t.Fatalf("stickersToAutoDownload(limit 0) = %v, want empty", msgIDs(got))
	}
}

func TestSelectChatAutoDownloadsNewestStickers(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "123@s.whatsapp.net"
	m.conversations[jid] = theme.Conversation{JID: jid}
	m.chatMessages[jid] = stickerMsgs(jid, 15)

	m, _ = m.selectChat(jid)

	wa.mu.Lock()
	got := msgIDs(wa.downloads)
	wa.mu.Unlock()
	want := []string{"s14", "s13", "s12", "s11", "s10", "s9", "s8", "s7", "s6", "s5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("downloads = %v, want %v", got, want)
	}
}
