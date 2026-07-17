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
	SenderAlt    string `json:"SenderAlt"`
	RecipientAlt string `json:"RecipientAlt"`
	IsFromMe     bool   `json:"IsFromMe"`
	IsGroup      bool   `json:"IsGroup"`
	ID           string `json:"ID"`
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

	AudioMessage *struct {
		PTT bool `json:"ptt"`
	} `json:"audioMessage"`

	DocumentMessage *struct {
		Caption  string `json:"caption"`
		FileName string `json:"fileName"`
	} `json:"documentMessage"`

	StickerMessage *struct {
		URL string `json:"url"`
	} `json:"stickerMessage"`

	ContactMessage *struct {
		DisplayName string `json:"displayName"`
		VCard       string `json:"vcard"`
	} `json:"contactMessage"`

	LocationMessage *struct {
		DegreesLatitude  float64 `json:"degreesLatitude"`
		DegreesLongitude float64 `json:"degreesLongitude"`
		Name             string  `json:"name"`
		Address          string  `json:"address"`
	} `json:"locationMessage"`

	PollCreationMessage *struct {
		Name string `json:"name"`
	} `json:"pollCreationMessage"`

	ReactionMessage *struct {
		Text string `json:"text"`
	} `json:"reactionMessage"`
}

// ---------------------------------------------------------------------
// Extracción
// ---------------------------------------------------------------------

func extractLabelEvent(raw []byte) (LabelEvent, error) {
	var env evolutionEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return LabelEvent{}, fmt.Errorf("invalid json: %w", err)
	}

	instanceName := strings.TrimSpace(env.InstanceID)
	if instanceName == "" {
		instanceName = "default"
	}

	if env.Event == "LabelEdit" {
		var data struct {
			LabelID string `json:"LabelID"`
			Action  struct {
				Name    string `json:"name"`
				Color   int    `json:"color"`
				Deleted bool   `json:"deleted"`
			} `json:"Action"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return LabelEvent{}, err
		}
		return LabelEvent{
			InstanceName: instanceName,
			EventType:    env.Event,
			LabelID:      data.LabelID,
			Name:         data.Action.Name,
			Color:        data.Action.Color,
			Deleted:      data.Action.Deleted,
		}, nil
	} else if env.Event == "LabelAssociationChat" {
		var data struct {
			JID     string `json:"JID"`
			LabelID string `json:"LabelID"`
			Action  struct {
				Labeled bool `json:"labeled"`
			} `json:"Action"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return LabelEvent{}, err
		}
		return LabelEvent{
			InstanceName: instanceName,
			EventType:    env.Event,
			LabelID:      data.LabelID,
			ChatJID:      data.JID,
			Labeled:      data.Action.Labeled,
		}, nil
	}
	return LabelEvent{}, fmt.Errorf("not a label event: %s", env.Event)
}

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

	// Filtrar actualizaciones de Estados de WhatsApp (status@broadcast)
	// para que no saturen la bandeja del CRM ni creen conversaciones fantasma.
	if strings.HasPrefix(data.Info.Chat, "status@") || strings.HasPrefix(data.Info.Sender, "status@") {
		return MessageRecord{}, fmt.Errorf("%w: status broadcast", ErrIgnoredEvent)
	}

	var content messageContent
	_ = json.Unmarshal(data.Message, &content) // best-effort: la forma varía por tipo

	messageType, messageText, caption := classifyContent(data.Info, content)

	direction := "inbound"
	if data.Info.IsFromMe {
		direction = "outbound"
	}

	var senderJID, receiverJID, chatJID string
	if direction == "outbound" {
		senderJID = data.Info.Sender
		receiverJID = chooseFirstNonEmpty(data.Info.RecipientAlt, data.Info.Chat)
		chatJID = receiverJID // Chat es el destinatario (el cliente)
	} else {
		// En 1:1, Sender suele venir vacío o igual al contacto; en grupos,
		// Sender es el participante real que escribió el mensaje.
		senderJID = chooseFirstNonEmpty(data.Info.Sender, data.Info.Chat)
		receiverJID = data.Info.Chat
		chatJID = data.Info.Chat // Chat es el remitente (el cliente) o el grupo
	}

	var ts *time.Time
	if parsed, err := time.Parse(time.RFC3339, data.Info.Timestamp); err == nil {
		parsedUTC := parsed.UTC()
		ts = &parsedUTC
	}

	// Normalizar el chat JID para evitar duplicados por variantes (:38, etc.)
	normalizedChat := normalizeJID(chatJID)
	normalizedSender := normalizeJID(senderJID)
	normalizedReceiver := normalizeJID(receiverJID)

	peerPhone := normalizePhoneFromJID(normalizedChat)
	
	// Si el mensaje es de salida, el PushName pertenece a la empresa, no al cliente.
	// Lo vaciamos para evitar que la base de datos sobrescriba el nombre del cliente.
	effectivePushName := data.Info.PushName
	if direction == "outbound" {
		effectivePushName = ""
	}

	displayName := resolveDisplayName(effectivePushName, peerPhone, normalizedChat, data.Info.IsGroup)
	if direction == "outbound" {
		displayName = peerPhone
	}

	// Título amigable para la conversación
	conversationTitle := resolveConversationTitle(effectivePushName, peerPhone, normalizedChat, data.Info.IsGroup)

	record := MessageRecord{
		RawPayload:        json.RawMessage(cleanRaw),
		ReceivedAt:        time.Now().UTC(),
		InstanceName:      instanceName,
		EventType:         env.Event,
		ProviderMessageID: data.Info.ID,
		ChatJID:           normalizedChat,
		SenderJID:         normalizedSender,
		ReceiverJID:       normalizedReceiver,
		FromNumber:        normalizePhoneFromJID(normalizedSender),
		ToNumber:          normalizePhoneFromJID(normalizedReceiver),
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
		JID:         normalizedChat,
		PhoneNumber: peerPhone,
		DisplayName: displayName,
		PushName:    effectivePushName,
		IsGroup:     data.Info.IsGroup,
		RawData:     json.RawMessage(`{}`),
	}
	record.Conversation = ConversationRecord{
		ChatJID:    normalizedChat,
		ContactJID: normalizedChat,
		Title:      conversationTitle,
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
	// 1. Determinar el tipo de mensaje a partir de Info.MediaType / Info.Type
	switch {
	case info.MediaType != "":
		messageType = strings.ToLower(info.MediaType) // "image", "video", "audio", "document"
	case info.Type != "":
		messageType = strings.ToLower(info.Type) // "text" u otro tipo no mapeado a MediaType
	default:
		messageType = "unknown"
	}

	// 2. Refinar el tipo inspeccionando el contenido del mensaje cuando
	//    Evolution GO no lo clasifica correctamente (ej. stickers llegan
	//    como tipo "text" o "media" sin MediaType).
	if content.StickerMessage != nil {
		messageType = "sticker"
	} else if content.ContactMessage != nil && messageType != "image" && messageType != "video" {
		messageType = "contact"
	} else if content.LocationMessage != nil {
		messageType = "location"
	} else if content.PollCreationMessage != nil {
		messageType = "poll"
	}

	// 3. Extraer texto/caption según el tipo
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
	case "audio":
		if content.AudioMessage != nil && content.AudioMessage.PTT {
			text = "🎤 Mensaje de voz"
		} else {
			text = "🎵 Audio"
		}
	case "document":
		if content.DocumentMessage != nil {
			caption = content.DocumentMessage.Caption
			if caption == "" && content.DocumentMessage.FileName != "" {
				text = "📎 " + content.DocumentMessage.FileName
			}
		}
	case "sticker":
		text = "🏷️ Sticker"
	case "contact":
		if content.ContactMessage != nil && content.ContactMessage.DisplayName != "" {
			text = "👤 " + content.ContactMessage.DisplayName
		} else {
			text = "👤 Contacto"
		}
	case "location":
		if content.LocationMessage != nil && content.LocationMessage.Name != "" {
			text = "📍 " + content.LocationMessage.Name
		} else {
			text = "📍 Ubicación"
		}
	case "poll":
		if content.PollCreationMessage != nil && content.PollCreationMessage.Name != "" {
			text = "📊 " + content.PollCreationMessage.Name
		} else {
			text = "📊 Encuesta"
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
// normalizeJID normaliza un JID de WhatsApp para uso como clave de
// deduplicación. Quita el sufijo ":XX" que Evolution GO agrega al device
// ID, dejando solo "numero@servidor".
// Ej: "557499879409:38@s.whatsapp.net" → "557499879409@s.whatsapp.net"
func normalizeJID(jid string) string {
	if jid == "" {
		return ""
	}
	jid = strings.TrimSpace(jid)

	// Buscar el @ primero
	atIdx := strings.Index(jid, "@")
	if atIdx < 0 {
		// Sin @, solo quitar el ":XX" si existe
		if colonIdx := strings.Index(jid, ":"); colonIdx >= 0 {
			return jid[:colonIdx]
		}
		return jid
	}

	// Tiene @: quitar ":XX" de la parte local (antes del @)
	local := jid[:atIdx]
	server := jid[atIdx:] // incluye el @
	if colonIdx := strings.Index(local, ":"); colonIdx >= 0 {
		local = local[:colonIdx]
	}
	return local + server
}

// jidSuffix devuelve la parte después del @ en un JID.
// Ej: "51999@s.whatsapp.net" → "s.whatsapp.net"
func jidSuffix(jid string) string {
	if idx := strings.Index(jid, "@"); idx >= 0 {
		return jid[idx+1:]
	}
	return ""
}

// isSpecialJID detecta JIDs que no representan un teléfono personal.
func isSpecialJID(jid string) bool {
	suffix := jidSuffix(jid)
	switch suffix {
	case "newsletter", "broadcast", "g.us", "s.whatsapp.net":
		// s.whatsapp.net es personal, los demás son especiales
		return suffix != "s.whatsapp.net"
	}
	// status@broadcast es especial
	if strings.HasPrefix(jid, "status@") {
		return true
	}
	return false
}

// resolveDisplayName genera un nombre legible para el contacto.
// Para JIDs normales usa PushName o el teléfono.
// Para newsletters/broadcasts/grupos usa nombres descriptivos.
func resolveDisplayName(pushName, phone, jid string, isGroup bool) string {
	if pushName != "" {
		return pushName
	}
	suffix := jidSuffix(jid)
	switch suffix {
	case "newsletter":
		return "📢 Newsletter"
	case "broadcast":
		if strings.HasPrefix(jid, "status@") {
			return "📸 Estados"
		}
		return "📣 Difusión"
	case "g.us":
		if isGroup {
			return "👥 Grupo"
		}
		return phone
	}
	if phone != "" {
		return phone
	}
	return jid
}

// resolveConversationTitle genera un título para la conversación en el CRM.
func resolveConversationTitle(pushName, phone, jid string, isGroup bool) string {
	// Para chats 1:1 con push name, preferir push name como título
	suffix := jidSuffix(jid)
	switch suffix {
	case "s.whatsapp.net":
		if pushName != "" {
			return pushName
		}
		return phone
	case "newsletter":
		if pushName != "" {
			return pushName
		}
		return "📢 Newsletter"
	case "broadcast":
		if strings.HasPrefix(jid, "status@") {
			return "📸 Estados"
		}
		return "📣 Difusión"
	case "g.us":
		if pushName != "" {
			return pushName
		}
		if isGroup {
			return "👥 Grupo"
		}
		return phone
	}
	if pushName != "" {
		return pushName
	}
	if phone != "" {
		return phone
	}
	return jid
}

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
