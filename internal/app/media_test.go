package app

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/watui/watui/internal/core"
)

// stickerMsgs builds n sticker messages (s0..s{n-1}) in ascending time order,
// all downloadable (DirectPath set, MediaPath empty).
func stickerMsgs(jid string, n int) []core.Message {
	msgs := make([]core.Message, n)
	for i := range msgs {
		msgs[i] = core.Message{
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
	m.conversations[jid] = core.Conversation{JID: jid}
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

// setupPendingOpen puts an undownloaded image in an open chat and triggers
// handleMediaOpen so that m.pendingOpenMsgID is set.
func setupPendingOpen(t *testing.T) (Model, *recordingWA, string) {
	t.Helper()
	m, _, wa := newRecordingModel(t)
	m.statusBar.SetWidth(200)
	jid := "123@s.whatsapp.net"
	m.conversations[jid] = core.Conversation{JID: jid}
	m.chatMessages[jid] = []core.Message{{
		ID: "img1", ChatJID: jid, MediaType: "image", DirectPath: "/direct/img1",
		Timestamp: time.Unix(100, 0),
	}}
	if cmd := m.handleMediaOpen(jid, "img1"); cmd == nil {
		t.Fatal("handleMediaOpen() returned nil cmd, want download")
	}
	if m.pendingOpenMsgID != "img1" {
		t.Fatalf("pendingOpenMsgID = %q, want img1", m.pendingOpenMsgID)
	}
	return m, wa, jid
}

func TestMediaDownloadFailedClearsPendingAndReports(t *testing.T) {
	m, _, jid := setupPendingOpen(t)

	updated, cmd := m.Update(core.MediaDownloadFailed{
		ChatJID: jid, MessageID: "img1", Err: errors.New("boom"),
	})
	m = updated.(Model)

	if m.pendingOpenMsgID != "" {
		t.Fatalf("pendingOpenMsgID = %q, want cleared", m.pendingOpenMsgID)
	}
	if view := m.statusBar.View(); !strings.Contains(view, "Media download failed: boom") {
		t.Fatalf("status bar = %q, want failure message", view)
	}
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want clear-status timer")
	}
}

func TestMediaDownloadFailedBackgroundIsQuiet(t *testing.T) {
	m, _, jid := setupPendingOpen(t)

	// A background (e.g. sticker auto-download) failure for another message
	// must not touch the pending open or the status bar.
	updated, _ := m.Update(core.MediaDownloadFailed{
		ChatJID: jid, MessageID: "sticker9", Err: errors.New("boom"),
	})
	m = updated.(Model)

	if m.pendingOpenMsgID != "img1" {
		t.Fatalf("pendingOpenMsgID = %q, want img1 kept", m.pendingOpenMsgID)
	}
	if view := m.statusBar.View(); strings.Contains(view, "Media download failed") {
		t.Fatalf("status bar = %q, want no failure message", view)
	}
}

func TestMediaDownloadedOpensPending(t *testing.T) {
	m, wa, jid := setupPendingOpen(t)

	updated, _ := m.Update(core.MediaDownloaded{
		ChatJID: jid, MessageID: "img1", Path: "/cache/img1.jpg",
	})
	m = updated.(Model)

	if m.pendingOpenMsgID != "" {
		t.Fatalf("pendingOpenMsgID = %q, want cleared", m.pendingOpenMsgID)
	}
	wa.mu.Lock()
	opens := append([]openMediaCall(nil), wa.opens...)
	wa.mu.Unlock()
	want := []openMediaCall{{Path: "/cache/img1.jpg", MediaType: "image"}}
	if !reflect.DeepEqual(opens, want) {
		t.Fatalf("opens = %v, want %v", opens, want)
	}
}
