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
