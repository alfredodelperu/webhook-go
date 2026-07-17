package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMessagesHandlerReturnsStoredMessages(t *testing.T) {
	receivedAt := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	store := &fakeMessagesStore{rows: []MessageRecord{{
		ProviderMessageID: "1",
		EventType:         "messages.upsert",
		MessageType:       "conversation",
		MessageText:       "hola",
		ReceivedAt:        receivedAt,
	}}}
	h := &MessagesHandler{Store: store}

	req := httptest.NewRequest(http.MethodGet, "/messages?limit=10", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Count    int             `json:"count"`
		Messages []MessageRecord `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Count != 1 || len(payload.Messages) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Messages[0].MessageText != "hola" {
		t.Fatalf("message text = %q", payload.Messages[0].MessageText)
	}
}

type fakeMessagesStore struct {
	rows []MessageRecord
}

func (f *fakeMessagesStore) EnsureSchema(context.Context) error { return nil }
func (f *fakeMessagesStore) Save(context.Context, MessageRecord) error { return nil }
func (f *fakeMessagesStore) ListRecent(_ context.Context, _ int) ([]MessageRecord, error) {
	return append([]MessageRecord(nil), f.rows...), nil
}
func (f *fakeMessagesStore) Close() error { return nil }
func (f *fakeMessagesStore) MarkConversationRead(_ context.Context, _ int64) error { return nil }
