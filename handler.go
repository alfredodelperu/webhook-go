package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

type WebhookHandler struct {
	Store  Store
	Logger *log.Logger
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()
	// Evolution Go envía el base64 de los archivos multimedia en el webhook.
	// Un video de 16MB puede pesar más de 20MB en base64, así que aumentamos el límite a 50MB.
	body, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	record, err := extractRecord(body)
	if err != nil {
		if errors.Is(err, ErrIgnoredEvent) {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.Store.Save(ctx, record); err != nil {
		if h.Logger != nil {
			h.Logger.Printf("save message: %v", err)
		}
		http.Error(w, "failed to store message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "accepted",
		"message": "stored",
	})
}
