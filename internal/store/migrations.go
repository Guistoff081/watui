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
	if err != nil {
		return err
	}
	return s.addMediaColumns()
}

// addMediaColumns idempotently adds Phase 7 media columns to the messages table.
// ALTER TABLE ADD COLUMN is used (safe to run against new or existing databases).
func (s *Store) addMediaColumns() error {
	// Read which columns already exist.
	rows, err := s.db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	additions := []struct {
		col string
		def string
	}{
		{"media_type", "TEXT NOT NULL DEFAULT ''"},
		{"media_path", "TEXT NOT NULL DEFAULT ''"},
		{"mime_type", "TEXT NOT NULL DEFAULT ''"},
		{"file_name", "TEXT NOT NULL DEFAULT ''"},
		{"thumbnail", "BLOB"},
		{"media_width", "INTEGER NOT NULL DEFAULT 0"},
		{"media_height", "INTEGER NOT NULL DEFAULT 0"},
		{"duration", "INTEGER NOT NULL DEFAULT 0"},
		{"is_animated", "INTEGER NOT NULL DEFAULT 0"},
		{"direct_path", "TEXT NOT NULL DEFAULT ''"},
		{"media_key", "BLOB"},
		{"file_sha256", "BLOB"},
		{"file_enc_sha256", "BLOB"},
	}

	for _, a := range additions {
		if existing[a.col] {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE messages ADD COLUMN ` + a.col + ` ` + a.def); err != nil {
			return err
		}
	}
	return nil
}
