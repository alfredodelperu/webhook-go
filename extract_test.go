package main

import (
	"context"
	"testing"
)

func TestExtractRecordParsesEvolutionLikePayload(t *testing.T) {
	raw := []byte(`{
		"event":"messages.upsert",
		"data":{
			"key":{"id":"ABCD123","remoteJid":"51999999999@s.whatsapp.net"},
			"pushName":"Felix Vilchez",
			"message":{"conversation":"Hola, quiero precio"},
			"messageTimestamp":1710000000
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}

	if record.EventType != "messages.upsert" {
		t.Fatalf("EventType = %q", record.EventType)
	}
	if record.ProviderMessageID != "ABCD123" {
		t.Fatalf("ProviderMessageID = %q", record.ProviderMessageID)
	}
	if record.FromNumber != "51999999999" {
		t.Fatalf("FromNumber = %q", record.FromNumber)
	}
	if record.SenderName != "Felix Vilchez" {
		t.Fatalf("SenderName = %q", record.SenderName)
	}
	if record.MessageText != "Hola, quiero precio" {
		t.Fatalf("MessageText = %q", record.MessageText)
	}
	if record.MessageType != "conversation" {
		t.Fatalf("MessageType = %q", record.MessageType)
	}
	if record.MessageTimestamp == nil {
		t.Fatal("MessageTimestamp = nil")
	}
}

func TestExtractRecordParsesOutboundTextPayload(t *testing.T) {
	raw := []byte(`{
		"event":"messages.upsert",
		"data":{
			"key":{"id":"OUT123","remoteJid":"51988888888@s.whatsapp.net","fromMe":true},
			"pushName":"DTF UV Perú",
			"message":{"conversation":"Sí, lo tenemos listo"},
			"messageTimestamp":1710000100
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.Direction != "outbound" {
		t.Fatalf("Direction = %q", record.Direction)
	}
	if record.ReceiverJID != "51988888888@s.whatsapp.net" {
		t.Fatalf("ReceiverJID = %q", record.ReceiverJID)
	}
	if record.ToNumber != "51988888888" {
		t.Fatalf("ToNumber = %q", record.ToNumber)
	}
	if record.MessageText != "Sí, lo tenemos listo" {
		t.Fatalf("MessageText = %q", record.MessageText)
	}
	if record.Conversation.Title != "51988888888" {
		t.Fatalf("Conversation.Title = %q", record.Conversation.Title)
	}
}

func TestExtractRecordInfersOutboundWhenSenderDiffersFromChat(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"data":{
			"Info":{"Chat":"51971261549@s.whatsapp.net","Sender":"227994228547703@lid"},
			"message":{"conversation":"Hola, revisa por favor"},
			"pushName":"fc galeriapuno",
			"messageTimestamp":1710000400
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.Direction != "outbound" {
		t.Fatalf("Direction = %q", record.Direction)
	}
	if record.ReceiverJID != "51971261549@s.whatsapp.net" {
		t.Fatalf("ReceiverJID = %q", record.ReceiverJID)
	}
	if record.Conversation.Title != "51971261549" {
		t.Fatalf("Conversation.Title = %q", record.Conversation.Title)
	}
	if record.Contact.DisplayName != "51971261549" {
		t.Fatalf("Contact.DisplayName = %q", record.Contact.DisplayName)
	}
}

func TestExtractRecordParsesOutboundButtonsResponse(t *testing.T) {
	raw := []byte(`{
		"event":"messages.upsert",
		"data":{
			"key":{"id":"OUT234","remoteJid":"51988888888@s.whatsapp.net","fromMe":true},
			"pushName":"DTF UV Perú",
			"message":{"buttonsResponseMessage":{"selectedDisplayText":"Confirmado"}},
			"messageTimestamp":1710000200
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.MessageText != "Confirmado" {
		t.Fatalf("MessageText = %q", record.MessageText)
	}
}

func TestExtractRecordParsesOutboundProtocolEditedMessage(t *testing.T) {
	raw := []byte(`{
		"event":"messages.upsert",
		"data":{
			"key":{"id":"OUT345","remoteJid":"51988888888@s.whatsapp.net","fromMe":true},
			"pushName":"DTF UV Perú",
			"message":{
				"protocolMessage":{
					"editedMessage":{
						"conversation":"Sí, te lo confirmo"
					}
				}
			},
			"messageTimestamp":1710000300
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.MessageType != "protocolMessage" {
		t.Fatalf("MessageType = %q", record.MessageType)
	}
	if record.MessageText != "Sí, te lo confirmo" {
		t.Fatalf("MessageText = %q", record.MessageText)
	}
	if record.Direction != "outbound" {
		t.Fatalf("Direction = %q", record.Direction)
	}
}

func TestExtractRecordDefaultsUnknownMessageType(t *testing.T) {
	raw := []byte(`{
		"event":"Message",
		"data":{
			"key":{"id":"NO-TYPE","remoteJid":"51999999999@s.whatsapp.net"},
			"pushName":"Cliente",
			"messageTimestamp":1710000500
		}
	}`)

	record, err := extractRecord(raw)
	if err != nil {
		t.Fatalf("extractRecord() error = %v", err)
	}
	if record.MessageType != "unknown" {
		t.Fatalf("MessageType = %q", record.MessageType)
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
