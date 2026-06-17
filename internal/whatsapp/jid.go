package whatsapp

import (
	"context"

	"go.mau.fi/whatsmeow/types"
)

func isMultiUserChat(jid types.JID) bool {
	switch jid.Server {
	case types.GroupServer, types.BroadcastServer, types.NewsletterServer:
		return true
	default:
		return false
	}
}

// canonicalChatJID returns a stable chat key for 1:1 chats, preferring the phone
// number JID when a LID↔PN mapping is known. Groups and newsletters are unchanged.
func (c *Client) canonicalChatJID(chat, alt types.JID) types.JID {
	chat = chat.ToNonAD()
	alt = alt.ToNonAD()

	if isMultiUserChat(chat) {
		return chat
	}

	ctx := context.Background()

	if chat.Server == types.HiddenUserServer && alt.Server == types.DefaultUserServer {
		c.wm.StoreLIDPNMapping(ctx, chat, alt)
	} else if chat.Server == types.DefaultUserServer && alt.Server == types.HiddenUserServer {
		c.wm.StoreLIDPNMapping(ctx, alt, chat)
	}

	if chat.Server == types.DefaultUserServer {
		return chat
	}

	if chat.Server == types.HiddenUserServer {
		if pn, err := c.wm.Store.LIDs.GetPNForLID(ctx, chat); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD()
		}
		if alt.Server == types.DefaultUserServer {
			return alt
		}
	}

	if alt.Server == types.DefaultUserServer {
		return alt
	}

	return chat
}

func (c *Client) canonicalChatFromInfo(info types.MessageInfo) types.JID {
	alt := types.EmptyJID
	if info.IsFromMe {
		alt = info.RecipientAlt
	} else {
		alt = info.SenderAlt
	}
	return c.canonicalChatJID(info.Chat, alt)
}

func (c *Client) canonicalSenderFromInfo(info types.MessageInfo) types.JID {
	if info.IsGroup {
		return info.Sender.ToNonAD()
	}
	return c.canonicalChatJID(info.Sender, info.SenderAlt)
}

// AltChatJID returns the alternate address (LID↔PN) for a 1:1 chat JID string.
func (c *Client) AltChatJID(jidStr string) string {
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return ""
	}
	alt, err := c.wm.Store.GetAltJID(context.Background(), jid.ToNonAD())
	if err != nil || alt.IsEmpty() {
		return ""
	}
	return alt.String()
}