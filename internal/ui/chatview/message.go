package chatview

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

// renderMessage renders a single message bubble. isSelected adds a highlight
// border when the chatview selection cursor is on this message. thumbCache is
// consulted (and populated) for expensive thumbnail renders.
func renderMessage(msg theme.Message, width int, isGroup bool, isSelected bool, thumbCache map[string]string) string {
	maxBubbleW := int(float64(width) * 0.7)
	if maxBubbleW < 20 {
		maxBubbleW = 20
	}

	timeStr := msg.Timestamp.Format("15:04")
	statusIcon := statusToIcon(msg.Status)

	var bubbleContent strings.Builder

	// Group sender name
	if isGroup && !msg.IsFromMe && msg.SenderName != "" {
		senderColor := getSenderColor(msg.SenderJID)
		senderStyle := lipgloss.NewStyle().Foreground(senderColor).Bold(true)
		bubbleContent.WriteString(senderStyle.Render(msg.SenderName))
		bubbleContent.WriteString("\n")
	}

	// Media or text body
	if msg.MediaType != "" {
		bubbleContent.WriteString(renderMediaBody(msg, maxBubbleW, thumbCache))
		bubbleContent.WriteString("\n")
	} else {
		bubbleContent.WriteString(msg.Content)
	}

	// Timestamp + status
	meta := theme.TimestampStyle.Render(timeStr)
	if msg.IsFromMe {
		meta += " " + statusIcon
	}
	bubbleContent.WriteString("  ")
	bubbleContent.WriteString(meta)

	var style lipgloss.Style
	if msg.IsFromMe {
		style = theme.MyMessageStyle.MaxWidth(maxBubbleW)
	} else {
		style = theme.OtherMessageStyle.MaxWidth(maxBubbleW)
	}

	if isSelected {
		style = style.BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(theme.ColorPrimary)
	}

	bubble := style.Render(bubbleContent.String())

	if msg.IsFromMe {
		return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(bubble)
	}
	return bubble
}

// renderMediaBody returns the inline representation of a media message body.
func renderMediaBody(msg theme.Message, maxW int, thumbCache map[string]string) string {
	switch msg.MediaType {
	case "image", "video", "gif", "sticker":
		return renderImageBody(msg, maxW, thumbCache)
	case "audio", "voice":
		return renderAudioBody(msg)
	case "document":
		return renderDocumentBody(msg)
	default:
		return "[media]"
	}
}

func renderImageBody(msg theme.Message, maxW int, thumbCache map[string]string) string {
	var b strings.Builder

	// Thumbnail: prefer embedded thumbnail (images/videos) over downloaded file (stickers).
	thumbData := msg.Thumbnail
	if len(thumbData) == 0 && msg.MediaPath != "" {
		thumbData = nil // will be loaded inside thumbnailToHalfBlock via MediaPath
	}

	// thumbCols: fit within bubble, cap at 40 for readability
	thumbCols := maxW - 4
	if thumbCols > 40 {
		thumbCols = 40
	}
	if thumbCols < 8 {
		thumbCols = 8
	}

	var rendered string
	if len(thumbData) > 0 {
		rendered = cachedThumbnail(msg.ID, thumbData, msg.MimeType, thumbCols, thumbCache)
	} else if msg.MediaPath != "" {
		rendered = cachedThumbnailFromPath(msg.ID, msg.MediaPath, msg.MimeType, thumbCols, thumbCache)
	}

	if rendered != "" {
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	// Type tag + dimensions
	tag := mediaTag(msg)
	if msg.Width > 0 && msg.Height > 0 {
		tag += fmt.Sprintf(" %d×%d", msg.Width, msg.Height)
	}
	b.WriteString(lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render(tag))

	if msg.Content != "" {
		b.WriteString("\n")
		b.WriteString(msg.Content)
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("↵ open"))

	return b.String()
}

func renderAudioBody(msg theme.Message) string {
	var b strings.Builder
	icon := "🎵"
	if msg.MediaType == "voice" {
		icon = "🎤"
	}
	dur := formatDuration(msg.Duration)
	b.WriteString(fmt.Sprintf("%s  %s", icon, dur))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("↵ play"))
	return b.String()
}

func renderDocumentBody(msg theme.Message) string {
	var b strings.Builder
	name := msg.FileName
	if name == "" {
		name = "file"
	}
	b.WriteString("📄  " + name)
	if msg.Content != "" {
		b.WriteString("\n")
		b.WriteString(msg.Content)
	}
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("↵ open"))
	return b.String()
}

func mediaTag(msg theme.Message) string {
	switch msg.MediaType {
	case "image":
		return "[image]"
	case "video":
		return "[video]"
	case "gif":
		return "[GIF]"
	case "sticker":
		if msg.IsAnimated {
			return "[animated sticker]"
		}
		return "[sticker]"
	}
	return "[media]"
}

func formatDuration(secs int) string {
	if secs <= 0 {
		return "0:00"
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

func renderDateSeparator(t time.Time, width int) string {
	now := time.Now()
	var label string
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		label = "Today"
	} else if t.Year() == now.Year() && t.YearDay() == now.YearDay()-1 {
		label = "Yesterday"
	} else if now.Sub(t) < 7*24*time.Hour {
		label = t.Format("Monday")
	} else {
		label = t.Format("January 2, 2006")
	}

	sep := fmt.Sprintf(" %s ", label)
	return theme.DateSeparatorStyle.Width(width).Render(sep)
}

func statusToIcon(status string) string {
	switch status {
	case "sending":
		return lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("◷")
	case "sent":
		return lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("✓")
	case "delivered":
		return lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("✓✓")
	case "read":
		return lipgloss.NewStyle().Foreground(theme.ColorSent).Render("✓✓")
	case "failed":
		return lipgloss.NewStyle().Foreground(theme.ColorError).Render("✗")
	default:
		return ""
	}
}

func getSenderColor(jid string) lipgloss.Color {
	if jid == "" {
		return theme.SenderColors[0]
	}
	hash := 0
	for _, c := range jid {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return theme.SenderColors[hash%len(theme.SenderColors)]
}
