package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CORSMiddleware agrega headers CORS para que el frontend pueda
// llamar a los endpoints de la API desde el navegador.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type MessagesHandler struct {
	Store  Store
	Logger *log.Logger
}

func (h *MessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.Store.ListRecent(ctx, limit)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Printf("list messages: %v", err)
		}
		http.Error(w, "failed to query messages", http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []MessageRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count":    len(rows),
		"messages": rows,
	})
}

// ConversationReadHandler marca una conversación como leída
// (reset de unread_count a 0).
// POST /conversations/read?id=<conversation_id>
type ConversationReadHandler struct {
	Store  Store
	Logger *log.Logger
}

func (h *ConversationReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimSpace(r.URL.Query().Get("id"))
	if idStr == "" {
		http.Error(w, "missing 'id' query param", http.StatusBadRequest)
		return
	}
	conversationID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid 'id' query param", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.Store.MarkConversationRead(ctx, conversationID); err != nil {
		if h.Logger != nil {
			h.Logger.Printf("mark conversation read: %v", err)
		}
		http.Error(w, "failed to mark conversation as read", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "ok",
		"conversation_id": conversationID,
		"unread_count":    0,
	})
}
