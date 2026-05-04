package theme

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	ColorPrimary   = lipgloss.Color("#25D366") // WhatsApp green
	ColorSecondary = lipgloss.Color("#128C7E")
	ColorAccent    = lipgloss.Color("#075E54")
	ColorBg        = lipgloss.Color("#111B21")
	ColorBgPanel   = lipgloss.Color("#1F2C34")
	ColorBgInput   = lipgloss.Color("#2A3942")
	ColorText      = lipgloss.Color("#E9EDEF")
	ColorTextDim   = lipgloss.Color("#8696A0")
	ColorBorder    = lipgloss.Color("#2A3942")
	ColorFocused   = lipgloss.Color("#25D366")
	ColorUnread    = lipgloss.Color("#25D366")
	ColorSent      = lipgloss.Color("#53BDEB")
	ColorError     = lipgloss.Color("#FF6B6B")
	ColorMyMsg     = lipgloss.Color("#005C4B")
	ColorOtherMsg  = lipgloss.Color("#1F2C34")
	ColorTimestamp = lipgloss.Color("#667781")

	// Sender colors for group chats
	SenderColors = []lipgloss.Color{
		lipgloss.Color("#FF6B6B"),
		lipgloss.Color("#4ECDC4"),
		lipgloss.Color("#FFE66D"),
		lipgloss.Color("#A8E6CF"),
		lipgloss.Color("#FF8B94"),
		lipgloss.Color("#DDA0DD"),
		lipgloss.Color("#98D8C8"),
		lipgloss.Color("#F7DC6F"),
	}

	// Title bar
	TitleBarStyle = lipgloss.NewStyle().
			Background(ColorAccent).
			Foreground(ColorText).
			Bold(true).
			Padding(0, 1)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Background(ColorAccent).
			Foreground(ColorText).
			Padding(0, 1)

	StatusConnected = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	StatusDisconnected = lipgloss.NewStyle().
				Foreground(ColorError).
				Bold(true)

	// Chat list
	ChatListStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(ColorBorder)

	ChatListFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, true, false, false).
				BorderForeground(ColorFocused)

	ChatItemSelected = lipgloss.NewStyle().
				Background(ColorBgInput).
				Foreground(ColorText)

	ChatItemNormal = lipgloss.NewStyle().
			Foreground(ColorText)

	ChatItemName = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText)

	ChatItemPreview = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	ChatItemTime = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	ChatItemUnread = lipgloss.NewStyle().
			Foreground(ColorBg).
			Background(ColorUnread).
			Bold(true).
			Padding(0, 1)

	// Message view
	MessageViewStyle = lipgloss.NewStyle()

	MyMessageStyle = lipgloss.NewStyle().
			Background(ColorMyMsg).
			Foreground(ColorText).
			Padding(0, 1)

	OtherMessageStyle = lipgloss.NewStyle().
				Background(ColorOtherMsg).
				Foreground(ColorText).
				Padding(0, 1)

	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorTimestamp)

	DateSeparatorStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Align(lipgloss.Center).
				Bold(true)

	// Input
	InputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(ColorBorder)

	InputFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), true, false, false, false).
				BorderForeground(ColorFocused)

	// QR Auth
	QRStyle = lipgloss.NewStyle().
		Align(lipgloss.Center, lipgloss.Center)

	QRTitleStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true).
			Align(lipgloss.Center)

	QRSubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Align(lipgloss.Center)
)
