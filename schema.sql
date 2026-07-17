-- Core instance registry
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

-- Raw webhook audit trail
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

-- Contacts / chats list source for inboxes
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

-- Conversation state for CRM/inbox UI
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

-- Messages feed for Realtime inboxes
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

-- Attachment metadata for future media storage
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

ALTER TABLE IF EXISTS whatsapp_labels ADD COLUMN IF NOT EXISTS provider_label_id TEXT;

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
