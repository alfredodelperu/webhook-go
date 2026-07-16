package main

// extract.go — reescrito para reflejar EXACTAMENTE el esquema real de
// Evolution GO, confirmado contra la documentación oficial:
// https://docs.evolutionfoundation.com.br/evolution-go/webhooks
//
// Cambios respecto a la versión anterior:
//   1. Se reemplaza la búsqueda recursiva por clave genérica (findRecursive)
//      por decodificación en structs tipados. Elimina el no-determinismo
//      causado por el orden de iteración de mapas en Go y por colisiones
//      de nombres de clave (ej. "id" apareciendo en más de un nivel).
//   2. InstanceName ahora lee "instanceId" (el campo real), no "instance"/
//      "instanceName" (que nunca existieron en el payload real — por eso
//      antes siempre caía en "default").
//   3. FromMe ahora lee "Info.IsFromMe" (el campo real), no "fromMe" a
//      nivel raíz (que tampoco existe — por eso antes siempre era false).
//   4. El tipo de mensaje ya NO se adivina inspeccionando las claves de
//      "Message" (de ahí salía el "imageMessage" crudo en el CRM). Se lee
//      directo de "Info.MediaType" / "Info.Type", que Evolution GO ya
//      entrega limpio ("image", "video", "text", etc.).
//   5. Solo se procesan eventos "Message". Cualquier otro evento (Receipt,
//      Connected, QRCode, CallOffer, GroupInfo, ...) se ignora explícita-
//      mente en vez de colarse como una fila vacía en la tabla de mensajes.
//      IMPORTANTE: esto requiere un ajuste en handler.go — ver nota al
//      final del archivo.
//   6. IsGroup se lee directo de "Info.IsGroup" (Evolution GO ya lo
//      resuelve), en vez de inferirlo del sufijo del JID.
//   7. La deduplicación usa instanceName + Info.ID (identificador real y
//      estable de WhatsApp), sin necesidad de un hash de todo el payload
//      como respaldo.
//
// NOTA: los tipos MessageRecord, ContactRecord y ConversationRecord se
// asumen definidos en messages.go tal como en la versión anterior. Si sus
// campos difieren de lo que uso aquí, compártelos y ajusto el mapeo.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrIgnoredEvent señala que el payload es válido pero no es un evento de
// tipo "Message" (ej. Receipt, Connected, QRCode...). handler.go debe
// tratar esto como "recibido, nada que guardar" y responder 2xx — NO como
// un error 400, o Evolution GO reintentará 5 veces cada evento ignorado.
var ErrIgnoredEvent = errors.New("evento ignorado: no es un mensaje de chat")

// ---------------------------------------------------------------------
// Structs que reflejan el payload REAL de Evolution GO
// ---------------------------------------------------------------------

// evolutionEnvelope es la envoltura común a todos los eventos.
type evolutionEnvelope struct {
	Event         string          `json:"event"`
	State         string          `json:"state"` // solo presente en "Receipt"
	InstanceID    string          `json:"instanceId"`
	InstanceToken string          `json:"instanceToken"`
	Data          json.RawMessage `json:"data"`
}

// messageInfo refleja data.Info para eventos "Message".
type messageInfo struct {
	Chat      string `json:"Chat"`
	Sender    string `json:"Sender"`
	SenderAlt string `json:"SenderAlt"`
	IsFromMe  bool   `json:"IsFromMe"`
	IsGroup   bool   `json:"IsGroup"`
	ID        string `json:"ID"`
	Type      string `json:"Type"`      // "text" | "media"
	PushName  string `json:"PushName"`
	Timestamp string `json:"Timestamp"` // ISO 8601, ej. 2024-10-10T17:17:44-03:00
	MediaType string `json:"MediaType"` // "image" | "video" | "audio" | "document" | ""
}

type messageData struct {
	Info    messageInfo     `json:"Info"`
	Message json.RawMessage `json:"Message"`
	IsEdit  bool            `json:"IsEdit"`
}

// messageContent cubre las formas de "Message" que nos interesa mostrar
// como texto/caption en el inbox. Se puede ampliar según se necesiten
// más tipos (listResponseMessage, buttonsResponseMessage, etc.).
type messageContent struct {
	Conversation string `json:"conversation"`

	ExtendedTextMessage *struct {
		Text string `json:"text"`
	} `json:"extendedTextMessage"`

	ImageMessage *struct {
		Caption string `json:"caption"`
	} `json:"imageMessage"`

	VideoMessage *struct {
		Caption string `json:"caption"`
	} `json:"videoMessage"`

	DocumentMessage *struct {
		Caption  string `json:"caption"`
		FileName string `json:"fileName"`
	} `json:"documentMessage"`

	ReactionMessage *struct {
		Text string `json:"text"`
	} `json:"reactionMessage"`
}

// ---------------------------------------------------------------------
// Extracción
// ---------------------------------------------------------------------

func extractRecord(raw []byte) (MessageRecord, error) {
	cleanRaw, err := sanitizeWebhookPayload(raw)
	if err != nil {
		return MessageRecord{}, err
	}

	var env evolutionEnvelope
	if err := json.Unmarshal(cleanRaw, &env); err != nil {
		return MessageRecord{}, fmt.Errorf("invalid json: %w", err)
	}

	if strings.TrimSpace(env.Event) == "" {
		return MessageRecord{}, errors.New("payload sin campo 'event'")
	}

	instanceName := strings.TrimSpace(env.InstanceID)
	if instanceName == "" {
		instanceName = "default"
	}

	// Solo el evento "Message" tiene la forma Info/Message que esperamos.
	// Todo lo demás (Receipt, Connected, QRCode, CallOffer, GroupInfo,
	// JoinedGroup, NewsletterJoin, ...) se ignora explícitamente.
	if !strings.EqualFold(env.Event, "Message") {
		return MessageRecord{}, fmt.Errorf("%w: event=%s", ErrIgnoredEvent, env.Event)
	}

	var data messageData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return MessageRecord{}, fmt.Errorf("data de mensaje inválida: %w", err)
	}
	if strings.TrimSpace(data.Info.ID) == "" {
		return MessageRecord{}, errors.New("evento Message sin Info.ID")
	}

	var content messageContent
	_ = json.Unmarshal(data.Message, &content) // best-effort: la forma varía por tipo

	messageType, messageText, caption := classifyContent(data.Info, content)

	direction := "inbound"
	if data.Info.IsFromMe {
		direction = "outbound"
	}

	var senderJID, receiverJID string
	if direction == "outbound" {
		senderJID = data.Info.Sender
		receiverJID = data.Info.Chat
	} else {
		// En 1:1, Sender suele venir vacío o igual al contacto; en grupos,
		// Sender es el participante real que escribió el mensaje.
		senderJID = chooseFirstNonEmpty(data.Info.Sender, data.Info.Chat)
		receiverJID = data.Info.Chat
	}

	var ts *time.Time
	if parsed, err := time.Parse(time.RFC3339, data.Info.Timestamp); err == nil {
		parsedUTC := parsed.UTC()
		ts = &parsedUTC
	}

	peerPhone := normalizePhoneFromJID(data.Info.Chat)
	displayName := chooseFirstNonEmpty(data.Info.PushName, peerPhone)
	if direction == "outbound" {
		displayName = peerPhone
	}

	record := MessageRecord{
		RawPayload:        json.RawMessage(cleanRaw),
		ReceivedAt:        time.Now().UTC(),
		InstanceName:      instanceName,
		EventType:         env.Event,
		ProviderMessageID: data.Info.ID,
		ChatJID:           data.Info.Chat,
		SenderJID:         senderJID,
		ReceiverJID:       receiverJID,
		FromNumber:        normalizePhoneFromJID(senderJID),
		ToNumber:          normalizePhoneFromJID(receiverJID),
		SenderName:        data.Info.PushName,
		MessageType:       messageType,
		MessageText:       messageText,
		Caption:           caption,
		FromMe:            data.Info.IsFromMe,
		Direction:         direction,
		IsGroup:           data.Info.IsGroup,
		MessageTimestamp:  ts,
	}

	record.Contact = ContactRecord{
		JID:         data.Info.Chat,
		PhoneNumber: peerPhone,
		DisplayName: displayName,
		PushName:    data.Info.PushName,
		IsGroup:     data.Info.IsGroup,
		RawData:     json.RawMessage(`{}`),
	}
	record.Conversation = ConversationRecord{
		ChatJID:    data.Info.Chat,
		ContactJID: data.Info.Chat,
		Title:      peerPhone,
		Status:     "open",
		Metadata:   json.RawMessage(`{}`),
	}

	// Clave de deduplicación: instancia + ID real del mensaje. Es única y
	// estable entre reintentos — reemplaza al hash de payload completo que
	// se usaba antes como respaldo.
	record.EventFingerprint = fmt.Sprintf("%s:%s", instanceName, data.Info.ID)

	return record, nil
}

// classifyContent decide el tipo de mensaje leyendo los campos que
// Evolution GO YA entrega clasificados (Info.MediaType / Info.Type), en
// vez de adivinar inspeccionando las claves de "Message".
func classifyContent(info messageInfo, content messageContent) (messageType, text, caption string) {
	switch {
	case info.MediaType != "":
		messageType = strings.ToLower(info.MediaType) // "image", "video", "audio", "document"
	case info.Type != "":
		messageType = strings.ToLower(info.Type) // "text" u otro tipo no mapeado a MediaType
	default:
		messageType = "unknown"
	}

	switch messageType {
	case "text":
		if content.Conversation != "" {
			text = content.Conversation
		} else if content.ExtendedTextMessage != nil {
			text = content.ExtendedTextMessage.Text
		} else if content.ReactionMessage != nil {
			messageType = "reaction"
			text = content.ReactionMessage.Text
		}
	case "image":
		if content.ImageMessage != nil {
			caption = content.ImageMessage.Caption
		}
	case "video":
		if content.VideoMessage != nil {
			caption = content.VideoMessage.Caption
		}
	case "document":
		if content.DocumentMessage != nil {
			caption = content.DocumentMessage.Caption
		}
	}

	if text == "" {
		text = caption
	}
	return messageType, text, caption
}

// ---------------------------------------------------------------------
// Utilidades
// ---------------------------------------------------------------------
//
// NOTA: normalizePhoneFromJID y chooseFirstNonEmpty NO se redefinen aquí
// a propósito — store.go ya las define en el mismo paquete `main`.
// (El normalizePhoneFromJID de store.go no separa por ":", pero para los
// JIDs reales de Evolution GO como "557499879409:38@s.whatsapp.net" eso
// deja el sufijo ":38" pegado al número. Te lo señalo abajo como algo a
// decidir, no lo cambio yo aquí para no duplicar definiciones.)

// sanitizeWebhookPayload redacta campos sensibles (tokens/API keys) antes
// de guardar el payload crudo en la base de datos.
func sanitizeWebhookPayload(raw []byte) ([]byte, error) {
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	redacted := redactSensitiveFields(node)
	return json.Marshal(redacted)
}

func redactSensitiveFields(v any) any {
	switch node := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for k, child := range node {
			switch strings.ToLower(k) {
			case "instancetoken", "apikey", "api_key", "authorization", "token", "jwt_key", "secret":
				out[k] = "[REDACTED]"
			default:
				out[k] = redactSensitiveFields(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			out[i] = redactSensitiveFields(child)
		}
		return out
	default:
		return v
	}
}

// ---------------------------------------------------------------------
// AJUSTE REQUERIDO EN handler.go
// ---------------------------------------------------------------------
//
// Actualmente handler.go responde 400 ante cualquier error de
// extractRecord. Con este cambio, los eventos no-"Message" (Receipt,
// Connected, QRCode, etc.) devuelven ErrIgnoredEvent — que NO es un
// fallo, es un "no aplica". Si sigues respondiendo 400, Evolution GO
// reintentará esos eventos 5 veces innecesariamente.
//
// Cambio sugerido en ServeHTTP:
//
//   record, err := extractRecord(body)
//   if err != nil {
//       if errors.Is(err, ErrIgnoredEvent) {
//           w.WriteHeader(http.StatusOK) // recibido, nada que guardar
//           return
//       }
//       http.Error(w, "failed to parse payload", http.StatusBadRequest)
//       return
//   }
