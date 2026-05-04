package chatview

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

func renderMessage(msg theme.Message, width int, isGroup bool) string {
	maxBubbleW := int(float64(width) * 0.7)
	if maxBubbleW < 20 {
		maxBubbleW = 20
	}

	content := msg.Content
	timeStr := msg.Timestamp.Format("15:04")
	statusIcon := statusToIcon(msg.Status)

	// Build the bubble content
	var bubbleContent strings.Builder

	if isGroup && !msg.IsFromMe && msg.SenderName != "" {
		senderColor := getSenderColor(msg.SenderJID)
		senderStyle := lipgloss.NewStyle().Foreground(senderColor).Bold(true)
		bubbleContent.WriteString(senderStyle.Render(msg.SenderName))
		bubbleContent.WriteString("\n")
	}

	bubbleContent.WriteString(content)

	// Time + status on the same line, right-aligned
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

	bubble := style.Render(bubbleContent.String())

	// Align: right for own messages, left for others
	if msg.IsFromMe {
		return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(bubble)
	}
	return bubble
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
