package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/watui/watui/internal/theme"
)

// Store is the app-level SQLite store for conversations and messages.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the app-level SQLite database and runs migrations.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", dbPath))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertConversation creates or updates a conversation.
// Only updates fields that are non-zero to avoid overwriting good data with empty data.
func (s *Store) UpsertConversation(ctx context.Context, conv theme.Conversation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversations (jid, name, is_group, last_message, last_msg_time, unread_count, is_pinned)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			name        = CASE WHEN excluded.name != '' THEN excluded.name ELSE conversations.name END,
			is_group    = excluded.is_group,
			last_message = CASE WHEN excluded.last_msg_time > conversations.last_msg_time THEN excluded.last_message ELSE conversations.last_message END,
			last_msg_time = CASE WHEN excluded.last_msg_time > conversations.last_msg_time THEN excluded.last_msg_time ELSE conversations.last_msg_time END,
			unread_count = CASE WHEN excluded.unread_count > 0 THEN excluded.unread_count ELSE conversations.unread_count END,
			is_pinned   = excluded.is_pinned
	`, conv.JID, conv.Name, conv.IsGroup, conv.LastMessage, conv.LastMsgTime.Unix(), conv.UnreadCount, conv.IsPinned)
	return err
}

// UpdateConversationLastMsg updates just the last message and timestamp for a conversation.
func (s *Store) UpdateConversationLastMsg(ctx context.Context, jid, lastMsg string, lastTime time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE conversations SET last_message = ?, last_msg_time = ? WHERE jid = ?
	`, lastMsg, lastTime.Unix(), jid)
	return err
}

// IncrementUnread increments the unread count for a conversation.
func (s *Store) IncrementUnread(ctx context.Context, jid string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE conversations SET unread_count = unread_count + 1 WHERE jid = ?
	`, jid)
	return err
}

// ClearUnread resets the unread count for a conversation.
func (s *Store) ClearUnread(ctx context.Context, jid string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE conversations SET unread_count = 0 WHERE jid = ?
	`, jid)
	return err
}

// GetAllConversations returns all conversations ordered by pinned first, then last message time.
func (s *Store) GetAllConversations(ctx context.Context) ([]theme.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT jid, name, is_group, last_message, last_msg_time, unread_count, is_pinned
		FROM conversations
		ORDER BY is_pinned DESC, last_msg_time DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []theme.Conversation
	for rows.Next() {
		var conv theme.Conversation
		var lastMsgTime int64
		if err := rows.Scan(&conv.JID, &conv.Name, &conv.IsGroup, &conv.LastMessage, &lastMsgTime, &conv.UnreadCount, &conv.IsPinned); err != nil {
			return nil, err
		}
		if lastMsgTime > 0 {
			conv.LastMsgTime = time.Unix(lastMsgTime, 0)
		}
		convs = append(convs, conv)
	}
	return convs, rows.Err()
}

// InsertMessages inserts a batch of messages, ignoring duplicates.
func (s *Store) InsertMessages(ctx context.Context, messages []theme.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO messages (id, chat_jid, sender_jid, sender_name, content, timestamp, is_from_me, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, msg := range messages {
		_, err := stmt.ExecContext(ctx,
			msg.ID, msg.ChatJID, msg.SenderJID, msg.SenderName,
			msg.Content, msg.Timestamp.Unix(), msg.IsFromMe, msg.Status,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// InsertMessage inserts a single message, ignoring if it already exists.
func (s *Store) InsertMessage(ctx context.Context, msg theme.Message) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO messages (id, chat_jid, sender_jid, sender_name, content, timestamp, is_from_me, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ID, msg.ChatJID, msg.SenderJID, msg.SenderName, msg.Content, msg.Timestamp.Unix(), msg.IsFromMe, msg.Status)
	return err
}

// GetMessages returns messages for a chat, ordered by timestamp ascending.
func (s *Store) GetMessages(ctx context.Context, chatJID string, limit int) ([]theme.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chat_jid, sender_jid, sender_name, content, timestamp, is_from_me, status
		FROM messages
		WHERE chat_jid = ?
		ORDER BY timestamp ASC
		LIMIT ?
	`, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []theme.Message
	for rows.Next() {
		var msg theme.Message
		var ts int64
		if err := rows.Scan(&msg.ID, &msg.ChatJID, &msg.SenderJID, &msg.SenderName, &msg.Content, &ts, &msg.IsFromMe, &msg.Status); err != nil {
			return nil, err
		}
		msg.Timestamp = time.Unix(ts, 0)
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}

// UpdateMessageStatus updates the status of a message by ID.
func (s *Store) UpdateMessageStatus(ctx context.Context, msgID, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE messages SET status = ? WHERE id = ?
	`, status, msgID)
	return err
}
