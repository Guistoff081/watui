package store

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			jid          TEXT PRIMARY KEY,
			name         TEXT NOT NULL DEFAULT '',
			is_group     BOOLEAN NOT NULL DEFAULT 0,
			last_message TEXT NOT NULL DEFAULT '',
			last_msg_time INTEGER NOT NULL DEFAULT 0,
			unread_count INTEGER NOT NULL DEFAULT 0,
			is_pinned    BOOLEAN NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS messages (
			id          TEXT NOT NULL,
			chat_jid    TEXT NOT NULL,
			sender_jid  TEXT NOT NULL DEFAULT '',
			sender_name TEXT NOT NULL DEFAULT '',
			content     TEXT NOT NULL DEFAULT '',
			timestamp   INTEGER NOT NULL DEFAULT 0,
			is_from_me  BOOLEAN NOT NULL DEFAULT 0,
			status      TEXT NOT NULL DEFAULT 'received',
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES conversations(jid)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_chat_time ON messages(chat_jid, timestamp);
	`)
	return err
}
