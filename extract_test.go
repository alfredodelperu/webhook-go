package main

import (
	"context"
	"errors"
	"testing"
)

func TestExtractRecordParsesInboundMessage(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"instanceId":"puno118",
		"data":{
			"Info":{
				"Chat":"51999999999@s.whatsapp.net",
				"Sender":"51999999999@s.whatsapp.net",
				"IsFromMe":false,
				"IsGroup":false,
				"ID":"MSG-001",
				"Type":"text",
				"PushName":"Cliente A",
				"Timestamp":"2024-10-10T17:17:44-03:00",
				"MediaType":""
			},
			"Message":{
				"conversation":"Hola, quiero precio de DTF UV."
			}
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}

	if record.InstanceName != "puno118" {
		t.Fatalf("InstanceName = %q", record.InstanceName)
	}
	if record.EventType != "Message" {
		t.Fatalf("EventType = %q", record.EventType)
	}
	if record.ProviderMessageID != "MSG-001" {
		t.Fatalf("ProviderMessageID = %q", record.ProviderMessageID)
	}
	if record.Direction != "inbound" {
		t.Fatalf("Direction = %q", record.Direction)
	}
	if record.MessageType != "text" {
		t.Fatalf("MessageType = %q", record.MessageType)
	}
	if record.MessageText != "Hola, quiero precio de DTF UV." {
		t.Fatalf("MessageText = %q", record.MessageText)
	}
	if record.Contact.DisplayName != "Cliente A" {
		t.Fatalf("Contact.DisplayName = %q", record.Contact.DisplayName)
	}
	if record.EventFingerprint != "puno118:MSG-001" {
		t.Fatalf("EventFingerprint = %q", record.EventFingerprint)
	}
	if record.MessageTimestamp == nil {
		t.Fatal("MessageTimestamp = nil")
	}
}

func TestExtractRecordParsesOutboundImageCaption(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"instanceId":"puno118",
		"data":{
			"Info":{
				"Chat":"51988888888@s.whatsapp.net",
				"Sender":"51910827777@s.whatsapp.net",
				"IsFromMe":true,
				"IsGroup":false,
				"ID":"MSG-002",
				"Type":"media",
				"PushName":"DTF UV Perú",
				"Timestamp":"2024-10-10T17:18:44-03:00",
				"MediaType":"image"
			},
			"Message":{
				"imageMessage":{
					"caption":"Te envío la referencia"
				}
			}
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}

	if record.Direction != "outbound" {
		t.Fatalf("Direction = %q", record.Direction)
	}
	if record.MessageType != "image" {
		t.Fatalf("MessageType = %q", record.MessageType)
	}
	if record.Caption != "Te envío la referencia" {
		t.Fatalf("Caption = %q", record.Caption)
	}
	if record.MessageText != "Te envío la referencia" {
		t.Fatalf("MessageText = %q", record.MessageText)
	}
	if record.Contact.DisplayName != "51988888888" {
		t.Fatalf("Contact.DisplayName = %q", record.Contact.DisplayName)
	}
}

func TestExtractRecordIgnoresNonMessageEvents(t *testing.T) {
	raw := []byte(`{
		"event":"Receipt",
		"instanceId":"puno118",
		"data":{
			"State":"delivered"
		}
	}`)

	_, err := extractRecord(raw)
	if !errors.Is(err, ErrIgnoredEvent) {
		t.Fatalf("expected ErrIgnoredEvent, got %v", err)
	}
}

func TestExtractRecordStickerMessage(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"instanceId":"puno118",
		"data":{
			"Info":{
				"Chat":"51999999999@s.whatsapp.net",
				"Sender":"51999999999@s.whatsapp.net",
				"IsFromMe":false,
				"IsGroup":false,
				"ID":"MSG-STICKER-001",
				"Type":"text",
				"PushName":"Cliente B",
				"Timestamp":"2024-10-10T17:20:00-03:00",
				"MediaType":""
			},
			"Message":{
				"stickerMessage":{
					"url":"https://example.com/sticker.webp"
				}
			}
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.MessageType != "sticker" {
		t.Fatalf("MessageType = %q, want sticker", record.MessageType)
	}
	if record.MessageText != "🏷️ Sticker" {
		t.Fatalf("MessageText = %q, want sticker emoji text", record.MessageText)
	}
}

func TestExtractRecordNewsletterDisplayName(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"instanceId":"puno118",
		"data":{
			"Info":{
				"Chat":"120363408806567029@newsletter",
				"Sender":"120363408806567029@newsletter",
				"IsFromMe":false,
				"IsGroup":false,
				"ID":"MSG-NL-001",
				"Type":"text",
				"PushName":"",
				"Timestamp":"2024-10-10T17:20:00-03:00",
				"MediaType":""
			},
			"Message":{
				"conversation":"Newsletter message"
			}
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.Contact.DisplayName != "📢 Newsletter" {
		t.Fatalf("Contact.DisplayName = %q, want newsletter label", record.Contact.DisplayName)
	}
	if record.Conversation.Title != "📢 Newsletter" {
		t.Fatalf("Conversation.Title = %q, want newsletter label", record.Conversation.Title)
	}
}

func TestNormalizeJID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"557499879409:38@s.whatsapp.net", "557499879409@s.whatsapp.net"},
		{"51999999999@s.whatsapp.net", "51999999999@s.whatsapp.net"},
		{"120363408806567029@newsletter", "120363408806567029@newsletter"},
		{"status@broadcast", "status@broadcast"},
		{"120363423116317343@g.us", "120363423116317343@g.us"},
		{"", ""},
		{"plain-text", "plain-text"},
		{"with:colon", "with"},
	}
	for _, tc := range tests {
		got := normalizeJID(tc.input)
		if got != tc.want {
			t.Errorf("normalizeJID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractRecordGroupConversationTitle(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"instanceId":"puno118",
		"data":{
			"Info":{
				"Chat":"120363423116317343@g.us",
				"Sender":"51999999999@s.whatsapp.net",
				"IsFromMe":false,
				"IsGroup":true,
				"ID":"MSG-GRP-001",
				"Type":"text",
				"PushName":"Miembro del grupo",
				"Timestamp":"2024-10-10T17:20:00-03:00",
				"MediaType":""
			},
			"Message":{
				"conversation":"Hola a todos"
			}
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	// For groups, PushName is the participant, not the group name.
	// Conversation title should use the PushName since group name is not available.
	if record.Conversation.Title != "Miembro del grupo" {
		t.Fatalf("Conversation.Title = %q", record.Conversation.Title)
	}
	if !record.IsGroup {
		t.Fatal("IsGroup should be true")
	}
}

func TestExtractRecordAudioPTT(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"instanceId":"puno118",
		"data":{
			"Info":{
				"Chat":"51999999999@s.whatsapp.net",
				"Sender":"51999999999@s.whatsapp.net",
				"IsFromMe":false,
				"IsGroup":false,
				"ID":"MSG-AUDIO-001",
				"Type":"media",
				"PushName":"Cliente C",
				"Timestamp":"2024-10-10T17:20:00-03:00",
				"MediaType":"audio"
			},
			"Message":{
				"audioMessage":{
					"ptt":true
				}
			}
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.MessageType != "audio" {
		t.Fatalf("MessageType = %q, want audio", record.MessageType)
	}
	if record.MessageText != "🎤 Mensaje de voz" {
		t.Fatalf("MessageText = %q, want voice message text", record.MessageText)
	}
}

type fakeStore struct {
	saved []MessageRecord
}

func (f *fakeStore) EnsureSchema(context.Context) error { return nil }

func (f *fakeStore) Save(_ context.Context, record MessageRecord) error {
	f.saved = append(f.saved, record)
	return nil
}

func (f *fakeStore) ListRecent(_ context.Context, _ int) ([]MessageRecord, error) {
	return append([]MessageRecord(nil), f.saved...), nil
}

func (f *fakeStore) Close() error { return nil }
func (f *fakeStore) MarkConversationRead(_ context.Context, _ int64) error { return nil }
