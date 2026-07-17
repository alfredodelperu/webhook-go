package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type ContactRecord struct {
	JID         string          `json:"jid"`
	PhoneNumber string          `json:"phone_number"`
	DisplayName string          `json:"display_name"`
	PushName    string          `json:"push_name"`
	IsGroup     bool            `json:"is_group"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
}

type ConversationRecord struct {
	ChatJID   string          `json:"chat_jid"`
	ContactJID string         `json:"contact_jid"`
	Title     string          `json:"title"`
	Status    string          `json:"status"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type MessageRecord struct {
	InstanceName       string          `json:"instance_name"`
	EventFingerprint   string          `json:"event_fingerprint"`
	ProviderMessageID  string          `json:"provider_message_id"`
	EventType          string          `json:"event_type"`
	Direction          string          `json:"direction"`
	ChatJID            string          `json:"chat_jid"`
	SenderJID          string          `json:"sender_jid"`
	ReceiverJID        string          `json:"receiver_jid"`
	SenderName         string          `json:"sender_name"`
	FromNumber         string          `json:"from_number"`
	ToNumber           string          `json:"to_number"`
	MessageType        string          `json:"message_type"`
	MessageText        string          `json:"message_text"`
	Caption            string          `json:"caption"`
	MessageStatus      string          `json:"message_status"`
	MessageTimestamp   *time.Time      `json:"message_timestamp,omitempty"`
	RawPayload         json.RawMessage `json:"raw_payload"`
	ReceivedAt         time.Time       `json:"received_at"`
	IsGroup            bool            `json:"is_group"`
	FromMe             bool            `json:"from_me"`
	Contact            ContactRecord   `json:"contact"`
	Conversation       ConversationRecord `json:"conversation"`
}

type LabelEvent struct {
	InstanceName string
	EventType    string // "LabelEdit" or "LabelAssociationChat"
	LabelID      string
	Name         string
	Color        int
	Deleted      bool
	ChatJID      string
	Labeled      bool
}

type Store interface {
	EnsureSchema(context.Context) error
	Save(context.Context, MessageRecord) error
	SaveLabelEvent(context.Context, LabelEvent) error
	ListRecent(context.Context, int) ([]MessageRecord, error)
	MarkConversationRead(ctx context.Context, conversationID int64) error
	Close() error
}

type PostgresStore struct {
	db *sql.DB
}

func OpenPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PostgresStore) EnsureSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS whatsapp_instances (
    id BIGSERIAL PRIMARY KEY,
    instance_name TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL DEFAULT 'evolution-go',
    display_name TEXT,
    phone_number TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS whatsapp_events (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT REFERENCES whatsapp_instances(id) ON DELETE SET NULL,
    instance_name TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL DEFAULT '',
    event_fingerprint TEXT NOT NULL UNIQUE,
    provider_message_id TEXT,
    chat_jid TEXT,
    sender_jid TEXT,
    direction TEXT NOT NULL DEFAULT '',
    message_type TEXT NOT NULL DEFAULT '',
    message_text TEXT,
    message_timestamp TIMESTAMPTZ,
    raw_payload JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS whatsapp_contacts (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES whatsapp_instances(id) ON DELETE CASCADE,
    jid TEXT NOT NULL,
    phone_number TEXT,
    display_name TEXT,
    push_name TEXT,
    is_group BOOLEAN NOT NULL DEFAULT FALSE,
    raw_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, jid)
);

CREATE TABLE IF NOT EXISTS whatsapp_conversations (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES whatsapp_instances(id) ON DELETE CASCADE,
    contact_id BIGINT REFERENCES whatsapp_contacts(id) ON DELETE SET NULL,
    chat_jid TEXT NOT NULL,
    contact_jid TEXT,
    title TEXT,
    last_message_fingerprint TEXT,
    last_message_text TEXT,
    last_message_at TIMESTAMPTZ,
    unread_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'open',
    assigned_to_user_id TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, chat_jid)
);

CREATE TABLE IF NOT EXISTS whatsapp_messages (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES whatsapp_instances(id) ON DELETE CASCADE,
    contact_id BIGINT REFERENCES whatsapp_contacts(id) ON DELETE SET NULL,
    conversation_id BIGINT REFERENCES whatsapp_conversations(id) ON DELETE SET NULL,
    event_fingerprint TEXT NOT NULL UNIQUE,
    provider_message_id TEXT,
    event_type TEXT NOT NULL DEFAULT '',
    direction TEXT NOT NULL DEFAULT '',
    chat_jid TEXT,
    sender_jid TEXT,
    receiver_jid TEXT,
    sender_name TEXT,
    message_type TEXT NOT NULL DEFAULT '',
    message_text TEXT,
    caption TEXT,
    message_status TEXT NOT NULL DEFAULT '',
    message_timestamp TIMESTAMPTZ,
    raw_payload JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS whatsapp_message_attachments (
    id BIGSERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES whatsapp_messages(id) ON DELETE CASCADE,
    attachment_kind TEXT NOT NULL DEFAULT '',
    mime_type TEXT,
    file_name TEXT,
    storage_path TEXT,
    public_url TEXT,
    size_bytes BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS instance_id BIGINT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS contact_id BIGINT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS conversation_id BIGINT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS event_fingerprint TEXT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS chat_jid TEXT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS sender_jid TEXT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS receiver_jid TEXT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS caption TEXT;
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS message_status TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS whatsapp_messages ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS whatsapp_messages ALTER COLUMN received_at SET DEFAULT NOW();

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'whatsapp_messages_provider_message_id_key'
    ) THEN
        ALTER TABLE whatsapp_messages DROP CONSTRAINT whatsapp_messages_provider_message_id_key;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS whatsapp_messages_provider_message_id_idx ON whatsapp_messages (provider_message_id);
CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_messages_event_fingerprint_uidx ON whatsapp_messages (event_fingerprint);

CREATE INDEX IF NOT EXISTS whatsapp_events_received_at_idx ON whatsapp_events (received_at DESC);
CREATE INDEX IF NOT EXISTS whatsapp_events_instance_idx ON whatsapp_events (instance_id, received_at DESC);
CREATE INDEX IF NOT EXISTS whatsapp_events_event_type_idx ON whatsapp_events (event_type);

CREATE INDEX IF NOT EXISTS whatsapp_contacts_instance_idx ON whatsapp_contacts (instance_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS whatsapp_contacts_jid_idx ON whatsapp_contacts (jid);

CREATE INDEX IF NOT EXISTS whatsapp_conversations_instance_idx ON whatsapp_conversations (instance_id, last_message_at DESC);
CREATE INDEX IF NOT EXISTS whatsapp_conversations_chat_jid_idx ON whatsapp_conversations (chat_jid);
CREATE INDEX IF NOT EXISTS whatsapp_conversations_unread_idx ON whatsapp_conversations (instance_id, unread_count DESC);

CREATE INDEX IF NOT EXISTS whatsapp_messages_instance_received_idx ON whatsapp_messages (instance_id, received_at DESC);
CREATE INDEX IF NOT EXISTS whatsapp_messages_conversation_idx ON whatsapp_messages (conversation_id, message_timestamp DESC);
CREATE INDEX IF NOT EXISTS whatsapp_messages_contact_idx ON whatsapp_messages (contact_id, message_timestamp DESC);
CREATE INDEX IF NOT EXISTS whatsapp_messages_event_type_idx ON whatsapp_messages (event_type);
CREATE INDEX IF NOT EXISTS whatsapp_messages_provider_message_id_idx ON whatsapp_messages (provider_message_id);
CREATE INDEX IF NOT EXISTS whatsapp_attachments_message_idx ON whatsapp_message_attachments (message_id);

CREATE OR REPLACE VIEW whatsapp_inbox AS
SELECT
    c.id AS conversation_id,
    c.instance_id,
    i.instance_name,
    c.chat_jid,
    c.contact_jid,
    c.title,
    c.last_message_fingerprint,
    c.last_message_text,
    c.last_message_at,
    c.unread_count,
    c.status,
    c.assigned_to_user_id,
    c.metadata,
    c.created_at,
    c.updated_at,
    ct.id AS contact_id,
    ct.jid AS contact_jid_ref,
    ct.phone_number,
    ct.display_name,
    ct.push_name,
    ct.is_group
FROM whatsapp_conversations c
JOIN whatsapp_instances i ON i.id = c.instance_id
LEFT JOIN whatsapp_contacts ct ON ct.id = c.contact_id;

ALTER TABLE whatsapp_events REPLICA IDENTITY FULL;
ALTER TABLE whatsapp_contacts REPLICA IDENTITY FULL;
ALTER TABLE whatsapp_conversations REPLICA IDENTITY FULL;
ALTER TABLE whatsapp_messages REPLICA IDENTITY FULL;
`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

func (s *PostgresStore) SaveLabelEvent(ctx context.Context, ev LabelEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var instanceID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM whatsapp_instances WHERE instance_name = $1", ev.InstanceName).Scan(&instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Upsert instance minimally just to get ID
			err = tx.QueryRowContext(ctx, "INSERT INTO whatsapp_instances (instance_name, metadata) VALUES ($1, '{}'::jsonb) ON CONFLICT (instance_name) DO UPDATE SET updated_at = NOW() RETURNING id", ev.InstanceName).Scan(&instanceID)
			if err != nil {
				return fmt.Errorf("failed to create instance for label: %w", err)
			}
		} else {
			return err
		}
	}

	if ev.EventType == "LabelEdit" {
		if ev.Deleted {
			_, err = tx.ExecContext(ctx, "DELETE FROM whatsapp_labels WHERE instance_id = $1 AND provider_label_id = $2", instanceID, ev.LabelID)
		} else {
			const q = `
INSERT INTO whatsapp_labels (instance_id, name, color, metadata, created_at, updated_at, provider_label_id)
VALUES ($1, $2, $3, '{}'::jsonb, NOW(), NOW(), $4)
ON CONFLICT (instance_id, name) DO UPDATE SET
    color = EXCLUDED.color,
    provider_label_id = EXCLUDED.provider_label_id,
    updated_at = NOW()
`
			_, err = tx.ExecContext(ctx, q, instanceID, ev.Name, ev.Color, ev.LabelID)
		}
		if err != nil {
			return fmt.Errorf("failed to upsert LabelEdit: %w", err)
		}
	} else if ev.EventType == "LabelAssociationChat" {
		// Encontrar el conversation_id buscando el JID del chat en whatsapp_messages, porque el JID puede ser @lid
		// y no sabemos a qué conversación pertenece directamente si whatsapp_conversations usa @s.whatsapp.net
		var conversationID int64
		err = tx.QueryRowContext(ctx, `
			SELECT c.id
			FROM whatsapp_conversations c
			WHERE c.instance_id = $1 AND c.chat_jid = $2
			LIMIT 1
		`, instanceID, ev.ChatJID).Scan(&conversationID)

		if err != nil && errors.Is(err, sql.ErrNoRows) {
			// Intentar buscar en los raw_payloads de whatsapp_messages
			err = tx.QueryRowContext(ctx, `
				SELECT conversation_id
				FROM whatsapp_messages
				WHERE instance_id = $1 AND (
					raw_payload->'data'->'Info'->>'SenderAlt' = $2 OR
					raw_payload->'data'->'Info'->>'RecipientAlt' = $2 OR
					raw_payload->'data'->'Info'->>'Chat' = $2 OR
					raw_payload->'data'->'Info'->>'Sender' = $2
				)
				LIMIT 1
			`, instanceID, ev.ChatJID).Scan(&conversationID)
		}

		if err == nil && conversationID > 0 {
			var labelID int64
			err = tx.QueryRowContext(ctx, "SELECT id FROM whatsapp_labels WHERE instance_id = $1 AND provider_label_id = $2", instanceID, ev.LabelID).Scan(&labelID)
			if err == nil {
				if ev.Labeled {
					_, err = tx.ExecContext(ctx, `
						INSERT INTO whatsapp_chat_labels (instance_id, conversation_id, label_id, created_at)
						VALUES ($1, $2, $3, NOW())
						ON CONFLICT (conversation_id, label_id) DO NOTHING
					`, instanceID, conversationID, labelID)
				} else {
					_, err = tx.ExecContext(ctx, `
						DELETE FROM whatsapp_chat_labels
						WHERE instance_id = $1 AND conversation_id = $2 AND label_id = $3
					`, instanceID, conversationID, labelID)
				}
				if err != nil {
					return fmt.Errorf("failed to sync LabelAssociationChat: %w", err)
				}
			}
		}
	}

	return tx.Commit()
}

func (s *PostgresStore) Save(ctx context.Context, record MessageRecord) error {
	if len(record.RawPayload) == 0 {
		return errors.New("raw payload is required")
	}
	if record.ReceivedAt.IsZero() {
		record.ReceivedAt = time.Now().UTC()
	}
	if record.EventFingerprint == "" {
		record.EventFingerprint = fingerprintMessageRecord(record)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	instanceID, err := upsertInstance(ctx, tx, record)
	if err != nil {
		return err
	}

	contactID, err := upsertContact(ctx, tx, instanceID, record)
	if err != nil {
		return err
	}

	conversationID, err := upsertConversation(ctx, tx, instanceID, contactID, record)
	if err != nil {
		return err
	}

	if err := upsertRawEvent(ctx, tx, instanceID, record); err != nil {
		return err
	}

	inserted, err := upsertMessage(ctx, tx, instanceID, contactID, conversationID, record)
	if err != nil {
		return err
	}
	if inserted && record.Direction == "inbound" {
		if err := incrementConversationUnread(ctx, tx, conversationID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *PostgresStore) ListRecent(ctx context.Context, limit int) ([]MessageRecord, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}

	const q = `
SELECT
    COALESCE(m.event_fingerprint, ''),
    COALESCE(i.instance_name, ''),
    COALESCE(m.provider_message_id, ''),
    COALESCE(m.event_type, ''),
    COALESCE(m.direction, ''),
    COALESCE(m.chat_jid, ''),
    COALESCE(m.sender_jid, ''),
    COALESCE(m.receiver_jid, ''),
    COALESCE(m.sender_name, ''),
    COALESCE(m.message_type, ''),
    COALESCE(m.message_text, ''),
    COALESCE(m.caption, ''),
    COALESCE(m.message_status, ''),
    m.message_timestamp,
    m.raw_payload,
    m.received_at,
    COALESCE(ct.jid, ''),
    COALESCE(ct.phone_number, ''),
    COALESCE(ct.display_name, ''),
    COALESCE(ct.push_name, ''),
    COALESCE(ct.is_group, false),
    COALESCE(c.chat_jid, ''),
    COALESCE(c.contact_jid, ''),
    COALESCE(c.title, ''),
    COALESCE(c.status, 'open')
FROM whatsapp_messages m
LEFT JOIN whatsapp_instances i ON i.id = m.instance_id
LEFT JOIN whatsapp_contacts ct ON ct.id = m.contact_id
LEFT JOIN whatsapp_conversations c ON c.id = m.conversation_id
ORDER BY m.received_at DESC, m.id DESC
LIMIT $1
`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []MessageRecord
	for rows.Next() {
		var (
			record             MessageRecord
			messageTimestamp   sql.NullTime
			rawPayload         []byte
			contactJID         sql.NullString
			contactPhoneNumber sql.NullString
			contactDisplayName sql.NullString
			contactPushName    sql.NullString
			contactIsGroup     sql.NullBool
			convChatJID        sql.NullString
			convContactJID     sql.NullString
			convTitle          sql.NullString
			convStatus         sql.NullString
		)
		if err := rows.Scan(
			&record.EventFingerprint,
			&record.InstanceName,
			&record.ProviderMessageID,
			&record.EventType,
			&record.Direction,
			&record.ChatJID,
			&record.SenderJID,
			&record.ReceiverJID,
			&record.SenderName,
			&record.MessageType,
			&record.MessageText,
			&record.Caption,
			&record.MessageStatus,
			&messageTimestamp,
			&rawPayload,
			&record.ReceivedAt,
			&contactJID,
			&contactPhoneNumber,
			&contactDisplayName,
			&contactPushName,
			&contactIsGroup,
			&convChatJID,
			&convContactJID,
			&convTitle,
			&convStatus,
		); err != nil {
			return nil, err
		}

		record.RawPayload = json.RawMessage(rawPayload)
		if messageTimestamp.Valid {
			t := messageTimestamp.Time.UTC()
			record.MessageTimestamp = &t
		}
		record.Contact = ContactRecord{
			JID:         contactJID.String,
			PhoneNumber: contactPhoneNumber.String,
			DisplayName: contactDisplayName.String,
			PushName:    contactPushName.String,
			IsGroup:     contactIsGroup.Bool,
		}
		record.Conversation = ConversationRecord{
			ChatJID:    convChatJID.String,
			ContactJID: convContactJID.String,
			Title:      convTitle.String,
			Status:     convStatus.String,
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func upsertInstance(ctx context.Context, tx *sql.Tx, record MessageRecord) (int64, error) {
	instanceDisplayName := ""
	instancePhoneNumber := ""
	if record.Direction == "outbound" {
		instanceDisplayName = record.SenderName
		instancePhoneNumber = record.FromNumber
	}
	const q = `
INSERT INTO whatsapp_instances (instance_name, provider, display_name, phone_number, metadata, updated_at)
VALUES ($1, 'evolution-go', NULLIF($2, ''), NULLIF($3, ''), '{}'::jsonb, NOW())
ON CONFLICT (instance_name) DO UPDATE SET
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), whatsapp_instances.display_name),
    phone_number = COALESCE(NULLIF(EXCLUDED.phone_number, ''), whatsapp_instances.phone_number),
    updated_at = NOW()
RETURNING id
`
	var id int64
	if err := tx.QueryRowContext(ctx, q, record.InstanceName, instanceDisplayName, instancePhoneNumber).Scan(&id); err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, errors.New("failed to upsert instance")
	}
	return id, nil
}

func upsertContact(ctx context.Context, tx *sql.Tx, instanceID int64, record MessageRecord) (int64, error) {
	contact := record.Contact
	if contact.JID == "" {
		contact.JID = record.ChatJID
	}
	if contact.PhoneNumber == "" {
		contact.PhoneNumber = normalizePhoneFromJID(contact.JID)
	}
	if contact.DisplayName == "" {
		contact.DisplayName = record.SenderName
	}
	if len(contact.RawData) == 0 {
		contact.RawData = json.RawMessage(`{}`)
	}
	const q = `
INSERT INTO whatsapp_contacts (instance_id, jid, phone_number, display_name, push_name, is_group, raw_data, updated_at)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, NOW())
ON CONFLICT (instance_id, jid) DO UPDATE SET
    phone_number = COALESCE(NULLIF(EXCLUDED.phone_number, ''), whatsapp_contacts.phone_number),
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), whatsapp_contacts.display_name),
    push_name = COALESCE(NULLIF(EXCLUDED.push_name, ''), whatsapp_contacts.push_name),
    is_group = EXCLUDED.is_group,
    raw_data = EXCLUDED.raw_data,
    updated_at = NOW()
RETURNING id
`
	var id int64
	if err := tx.QueryRowContext(ctx, q, instanceID, contact.JID, contact.PhoneNumber, contact.DisplayName, contact.PushName, contact.IsGroup, contact.RawData).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func upsertConversation(ctx context.Context, tx *sql.Tx, instanceID, contactID int64, record MessageRecord) (int64, error) {
	conversation := record.Conversation
	if conversation.ChatJID == "" {
		conversation.ChatJID = record.ChatJID
	}
	if conversation.ContactJID == "" {
		conversation.ContactJID = record.ChatJID
	}
	if conversation.Title == "" {
		conversation.Title = record.SenderName
	}
	if conversation.Status == "" {
		conversation.Status = "open"
	}
	if len(conversation.Metadata) == 0 {
		conversation.Metadata = json.RawMessage(`{}`)
	}
	previewText := chooseFirstNonEmpty(record.MessageText, record.Caption, record.MessageType)
	const q = `
INSERT INTO whatsapp_conversations (
    instance_id, contact_id, chat_jid, contact_jid, title,
    last_message_fingerprint, last_message_text, last_message_at,
    unread_count, status, metadata, updated_at
) VALUES (
    $1, $2, $3, NULLIF($4, ''), NULLIF($5, ''),
    $6, NULLIF($7, ''), $8,
    0, $9, $10, NOW()
)
ON CONFLICT (instance_id, chat_jid) DO UPDATE SET
    contact_id = COALESCE(EXCLUDED.contact_id, whatsapp_conversations.contact_id),
    contact_jid = COALESCE(NULLIF(EXCLUDED.contact_jid, ''), whatsapp_conversations.contact_jid),
    title = COALESCE(NULLIF(EXCLUDED.title, ''), whatsapp_conversations.title),
    last_message_fingerprint = EXCLUDED.last_message_fingerprint,
    last_message_text = COALESCE(NULLIF(EXCLUDED.last_message_text, ''), whatsapp_conversations.last_message_text),
    last_message_at = EXCLUDED.last_message_at,
    status = CASE WHEN whatsapp_conversations.status = 'closed' THEN 'open' ELSE whatsapp_conversations.status END,
    metadata = COALESCE(EXCLUDED.metadata, whatsapp_conversations.metadata),
    updated_at = NOW()
RETURNING id
`
	var id int64
	if err := tx.QueryRowContext(ctx, q,
		instanceID,
		contactID,
		record.ChatJID,
		record.Conversation.ContactJID,
		record.Conversation.Title,
		record.EventFingerprint,
		previewText,
		record.MessageTimestamp,
		record.Conversation.Status,
		record.Conversation.Metadata,
	).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func upsertRawEvent(ctx context.Context, tx *sql.Tx, instanceID int64, record MessageRecord) error {
	const q = `
INSERT INTO whatsapp_events (
    instance_id, instance_name, event_type, event_fingerprint,
    provider_message_id, chat_jid, sender_jid, direction,
    message_type, message_text, message_timestamp, raw_payload, received_at
) VALUES (
    $1, NULLIF($2, ''), NULLIF($3, ''), $4,
    NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''),
    COALESCE(NULLIF($9, ''), 'unknown'), NULLIF($10, ''), $11, $12, $13
)
ON CONFLICT (event_fingerprint) DO UPDATE SET
    instance_id = EXCLUDED.instance_id,
    instance_name = EXCLUDED.instance_name,
    event_type = EXCLUDED.event_type,
    provider_message_id = EXCLUDED.provider_message_id,
    chat_jid = EXCLUDED.chat_jid,
    sender_jid = EXCLUDED.sender_jid,
    direction = EXCLUDED.direction,
    message_type = EXCLUDED.message_type,
    message_text = EXCLUDED.message_text,
    message_timestamp = EXCLUDED.message_timestamp,
    raw_payload = EXCLUDED.raw_payload,
    received_at = EXCLUDED.received_at
`
	_, err := tx.ExecContext(ctx, q,
		instanceID,
		record.InstanceName,
		record.EventType,
		record.EventFingerprint,
		record.ProviderMessageID,
		record.ChatJID,
		record.SenderJID,
		record.Direction,
		record.MessageType,
		record.MessageText,
		record.MessageTimestamp,
		record.RawPayload,
		record.ReceivedAt,
	)
	return err
}

func upsertMessage(ctx context.Context, tx *sql.Tx, instanceID, contactID, conversationID int64, record MessageRecord) (bool, error) {
	const q = `
INSERT INTO whatsapp_messages (
    instance_id, contact_id, conversation_id, event_fingerprint, provider_message_id,
    event_type, direction, chat_jid, sender_jid, receiver_jid, sender_name,
    message_type, message_text, caption, message_status, message_timestamp,
    raw_payload, received_at, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, NULLIF($5, ''),
    NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''),
    COALESCE(NULLIF($12, ''), 'unknown'), NULLIF($13, ''), NULLIF($14, ''), $15, $16,
    $17, $18, NOW(), NOW()
)
ON CONFLICT (event_fingerprint) DO UPDATE SET
    instance_id = EXCLUDED.instance_id,
    contact_id = EXCLUDED.contact_id,
    conversation_id = EXCLUDED.conversation_id,
    provider_message_id = EXCLUDED.provider_message_id,
    event_type = EXCLUDED.event_type,
    direction = EXCLUDED.direction,
    chat_jid = EXCLUDED.chat_jid,
    sender_jid = EXCLUDED.sender_jid,
    receiver_jid = EXCLUDED.receiver_jid,
    sender_name = EXCLUDED.sender_name,
    message_type = EXCLUDED.message_type,
    message_text = COALESCE(NULLIF(EXCLUDED.message_text, ''), whatsapp_messages.message_text),
    caption = COALESCE(NULLIF(EXCLUDED.caption, ''), whatsapp_messages.caption),
    message_status = EXCLUDED.message_status,
    message_timestamp = EXCLUDED.message_timestamp,
    raw_payload = EXCLUDED.raw_payload,
    received_at = EXCLUDED.received_at,
    updated_at = NOW()
RETURNING id, (xmax = 0) AS inserted
`
	var (
		id       int64
		inserted bool
	)
	if err := tx.QueryRowContext(ctx, q,
		instanceID,
		contactID,
		conversationID,
		record.EventFingerprint,
		record.ProviderMessageID,
		record.EventType,
		record.Direction,
		record.ChatJID,
		record.SenderJID,
		record.ReceiverJID,
		record.SenderName,
		record.MessageType,
		record.MessageText,
		record.Caption,
		record.MessageStatus,
		record.MessageTimestamp,
		record.RawPayload,
		record.ReceivedAt,
	).Scan(&id, &inserted); err != nil {
		return false, err
	}
	_ = id
	return inserted, nil
}

func incrementConversationUnread(ctx context.Context, tx *sql.Tx, conversationID int64) error {
	const q = `
UPDATE whatsapp_conversations
SET unread_count = unread_count + 1,
    status = 'open',
    updated_at = NOW()
WHERE id = $1
`
	_, err := tx.ExecContext(ctx, q, conversationID)
	return err
}

func (s *PostgresStore) MarkConversationRead(ctx context.Context, conversationID int64) error {
	const q = `
UPDATE whatsapp_conversations
SET unread_count = 0,
    updated_at = NOW()
WHERE id = $1
`
	_, err := s.db.ExecContext(ctx, q, conversationID)
	return err
}

func fingerprintMessageRecord(record MessageRecord) string {
	h := sha256.New()
	parts := []string{
		record.InstanceName,
		record.EventType,
		record.ProviderMessageID,
		record.ChatJID,
		record.MessageText,
		record.Direction,
		record.MessageType,
	}
	_, _ = h.Write([]byte(strings.Join(parts, "|")))
	if len(record.RawPayload) > 0 {
		_, _ = h.Write(record.RawPayload)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func normalizePhoneFromJID(jid string) string {
	if jid == "" {
		return ""
	}
	jid = strings.TrimSpace(jid)
	if idx := strings.Index(jid, "@"); idx >= 0 {
		jid = jid[:idx]
	}
	if idx := strings.Index(jid, ":"); idx >= 0 {
		jid = jid[:idx]
	}
	return jid
}

func chooseFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
