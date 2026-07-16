package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type webhookEnvelope = map[string]any

func firstPathString(payload webhookEnvelope, paths ...[]string) string {
	for _, path := range paths {
		if v, ok := lookupPath(payload, path); ok {
			if s, ok := stringValue(v); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func firstChatJID(payload webhookEnvelope) string {
	return chooseFirstNonEmpty(
		firstPathString(payload,
			[]string{"data", "Info", "Chat"},
			[]string{"data", "Message", "key", "remoteJID"},
			[]string{"data", "Message", "key", "remoteJid"},
			[]string{"message", "key", "remoteJID"},
			[]string{"message", "key", "remoteJid"},
			[]string{"remoteJid"},
			[]string{"chatJid"},
			[]string{"chat_jid"},
			[]string{"jid"},
		),
		firstPathString(payload,
			[]string{"data", "Info", "Chat"},
		),
	)
}

func firstSenderJID(payload webhookEnvelope) string {
	return chooseFirstNonEmpty(
		firstPathString(payload,
			[]string{"data", "Info", "Sender"},
			[]string{"data", "Message", "key", "participant"},
			[]string{"data", "Message", "key", "participantJID"},
			[]string{"message", "key", "participant"},
			[]string{"message", "key", "participantJID"},
			[]string{"participant"},
			[]string{"senderJid"},
			[]string{"sender"},
			[]string{"from"},
			[]string{"waId"},
		),
		firstPathString(payload,
			[]string{"data", "Info", "Sender"},
		),
	)
}

func extractRecord(raw []byte) (MessageRecord, error) {
	cleanRaw, err := sanitizeWebhookPayload(raw)
	if err != nil {
		return MessageRecord{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(cleanRaw)))
	dec.UseNumber()
	var payload webhookEnvelope
	if err := dec.Decode(&payload); err != nil {
		return MessageRecord{}, fmt.Errorf("invalid json: %w", err)
	}

	record := MessageRecord{
		RawPayload:  json.RawMessage(cleanRaw),
		ReceivedAt:  time.Now().UTC(),
		InstanceName: firstString(payload, "instance", "instanceName", "instance_name"),
		EventType:    firstString(payload, "event", "eventType", "type"),
		ProviderMessageID: firstString(payload,
			"providerMessageId",
			"messageId",
			"id",
			"stanzaId",
		),
		ChatJID:   firstChatJID(payload),
		SenderJID: firstSenderJID(payload),
		FromNumber: normalizePhoneFromJID(firstChatJID(payload)),
		ToNumber:   normalizePhoneFromJID(firstSenderJID(payload)),
		SenderName: firstString(payload,
			"pushName",
			"push_name",
			"senderName",
			"name",
			"contactName",
			"displayName",
			"subject",
		),
		MessageType: firstMessageType(payload),
		MessageText:  chooseFirstNonEmpty(firstMessageText(payload), firstMessageInteractiveText(payload), firstMessageCaption(payload)),
		Caption:      firstMessageCaption(payload),
		MessageStatus: firstString(payload,
			"messageStatus",
			"status",
			"receiptStatus",
		),
		FromMe: firstBool(payload, "fromMe", "from_me"),
	}

	if record.EventType == "" {
		// Evolution GO emits MESSAGE, but older wrappers may send messages.upsert.
		record.EventType = firstString(payload, "event", "eventType", "type", "messageType")
	}
	if strings.TrimSpace(record.InstanceName) == "" {
		record.InstanceName = "default"
	}

	if record.ChatJID == "" {
		record.ChatJID = firstString(payload, "remoteJid", "chatJid", "jid")
	}
	if record.SenderJID == "" {
		record.SenderJID = record.ChatJID
	}
	if record.SenderName == "" {
		record.SenderName = firstString(payload, "pushName", "name", "displayName")
	}
	if record.MessageText == "" {
		record.MessageText = firstMessageText(payload)
	}
	if record.Caption == "" {
		record.Caption = firstString(payload, "caption")
	}
	if record.MessageType == "" {
		record.MessageType = messageTypeFromContent(payload)
	}
	if strings.TrimSpace(record.MessageType) == "" {
		record.MessageType = "unknown"
	}

	if ts := firstAny(payload, "messageTimestamp", "timestamp", "ts", "time"); ts != nil {
		parsed, err := parseAnyTime(ts)
		if err == nil {
			record.MessageTimestamp = &parsed
		}
	}

	if record.Direction == "" {
		record.Direction = deriveDirection(record)
	}
	if record.Direction == "" {
		record.Direction = "inbound"
	}

	if record.Direction == "outbound" {
		record.ReceiverJID = chooseFirstNonEmpty(record.ChatJID, firstString(payload, "data", "Info", "Chat"))
		if record.SenderJID == "" {
			record.SenderJID = chooseFirstNonEmpty(firstString(payload, "data", "Info", "Sender"), firstString(payload, "data", "Message", "key", "participant"), firstString(payload, "senderJid"))
		}
	} else {
		record.SenderJID = chooseFirstNonEmpty(record.ChatJID, record.SenderJID)
		if record.ReceiverJID == "" {
			record.ReceiverJID = chooseFirstNonEmpty(firstString(payload, "data", "Info", "Receiver"), firstString(payload, "data", "Info", "Chat"), firstString(payload, "data", "Message", "key", "remoteJID"))
		}
	}

	if record.FromNumber == "" {
		if record.Direction == "outbound" {
			record.FromNumber = normalizePhoneFromJID(record.SenderJID)
		} else {
			record.FromNumber = normalizePhoneFromJID(record.SenderJID)
		}
	}
	if record.ToNumber == "" {
		if record.Direction == "outbound" {
			record.ToNumber = normalizePhoneFromJID(record.ReceiverJID)
		} else {
			record.ToNumber = normalizePhoneFromJID(record.ReceiverJID)
		}
	}

	peerJID := chooseFirstNonEmpty(record.ChatJID, firstString(payload, "data", "Info", "Chat"))
	peerPhone := chooseFirstNonEmpty(normalizePhoneFromJID(peerJID), peerJID)
	record.IsGroup = strings.HasSuffix(strings.ToLower(peerJID), "@g.us")
	contactDisplayName := chooseFirstNonEmpty(record.SenderName, peerPhone)
	conversationTitle := peerPhone
	if record.Direction == "outbound" {
		contactDisplayName = peerPhone
	}
	record.Contact = ContactRecord{
		JID:         peerJID,
		PhoneNumber: normalizePhoneFromJID(peerJID),
		DisplayName: contactDisplayName,
		PushName:    record.SenderName,
		IsGroup:     record.IsGroup,
		RawData:     json.RawMessage(`{}`),
	}
	record.Conversation = ConversationRecord{
		ChatJID:    peerJID,
		ContactJID: peerJID,
		Title:      conversationTitle,
		Status:     "open",
		Metadata:   json.RawMessage(`{}`),
	}

	if record.EventFingerprint == "" {
		record.EventFingerprint = fingerprintFromPayload(record, cleanRaw)
	}

	if record.EventType == "" && record.MessageText == "" && record.ProviderMessageID == "" {
		return MessageRecord{}, errors.New("payload does not look like a WhatsApp message event")
	}

	return record, nil
}

func firstMessageType(payload webhookEnvelope) string {
	if s := firstString(payload, "messageType", "message_type", "kind"); s != "" {
		return s
	}
	if msg, ok := messageNode(payload); ok {
		for _, key := range []string{
			"conversation",
			"extendedTextMessage",
			"imageMessage",
			"videoMessage",
			"audioMessage",
			"documentMessage",
			"stickerMessage",
			"templateMessage",
			"buttonsResponseMessage",
			"listResponseMessage",
			"reactionMessage",
			"protocolMessage",
		} {
			for k := range msg {
				if strings.EqualFold(k, key) {
					return key
				}
			}
		}
	}
	return ""
}

func messageTypeFromContent(payload webhookEnvelope) string {
	if msg, ok := messageNode(payload); ok {
		for key := range msg {
			lower := strings.ToLower(key)
			switch lower {
			case "conversation", "extendedtextmessage", "imagemessage", "videomessage", "audiomessage", "documentmessage", "stickermessage", "templatemessage", "buttonsresponsemessage", "listresponsemessage", "reactionmessage", "protocolmessage":
				return key
			}
		}
	}
	return ""
}

func firstMessageText(payload webhookEnvelope) string {
	for _, path := range [][]string{
		{"message", "conversation"},
		{"data", "message", "conversation"},
		{"message", "extendedTextMessage", "text"},
		{"data", "message", "extendedTextMessage", "text"},
		{"message", "imageMessage", "caption"},
		{"data", "message", "imageMessage", "caption"},
		{"message", "videoMessage", "caption"},
		{"data", "message", "videoMessage", "caption"},
		{"message", "documentMessage", "caption"},
		{"data", "message", "documentMessage", "caption"},
		{"message", "templateMessage", "hydratedTemplate", "hydratedContentText"},
		{"data", "message", "templateMessage", "hydratedTemplate", "hydratedContentText"},
		{"message", "buttonsResponseMessage", "selectedButtonId"},
		{"data", "message", "buttonsResponseMessage", "selectedButtonId"},
		{"message", "buttonsResponseMessage", "selectedDisplayText"},
		{"data", "message", "buttonsResponseMessage", "selectedDisplayText"},
		{"message", "listResponseMessage", "singleSelectReply", "selectedDisplayText"},
		{"data", "message", "listResponseMessage", "singleSelectReply", "selectedDisplayText"},
		{"message", "listResponseMessage", "singleSelectReply", "selectedRowId"},
		{"data", "message", "listResponseMessage", "singleSelectReply", "selectedRowId"},
		{"message", "listResponseMessage", "title"},
		{"data", "message", "listResponseMessage", "title"},
		{"message", "interactiveResponseMessage", "body", "text"},
		{"data", "message", "interactiveResponseMessage", "body", "text"},
		{"message", "interactiveResponseMessage", "nativeFlowResponseMessage", "paramsJson"},
		{"data", "message", "interactiveResponseMessage", "nativeFlowResponseMessage", "paramsJson"},
		{"message", "templateButtonReplyMessage", "selectedDisplayText"},
		{"data", "message", "templateButtonReplyMessage", "selectedDisplayText"},
		{"message", "templateButtonReplyMessage", "selectedId"},
		{"data", "message", "templateButtonReplyMessage", "selectedId"},
		{"message", "questionResponseMessage", "text"},
		{"data", "message", "questionResponseMessage", "text"},
		{"message", "protocolMessage", "editedMessage", "conversation"},
		{"data", "message", "protocolMessage", "editedMessage", "conversation"},
		{"message", "protocolMessage", "editedMessage", "extendedTextMessage", "text"},
		{"data", "message", "protocolMessage", "editedMessage", "extendedTextMessage", "text"},
		{"message", "protocolMessage", "editedMessage", "imageMessage", "caption"},
		{"data", "message", "protocolMessage", "editedMessage", "imageMessage", "caption"},
		{"message", "protocolMessage", "editedMessage", "videoMessage", "caption"},
		{"data", "message", "protocolMessage", "editedMessage", "videoMessage", "caption"},
		{"message", "protocolMessage", "editedMessage", "documentMessage", "caption"},
		{"data", "message", "protocolMessage", "editedMessage", "documentMessage", "caption"},
	} {
		if v, ok := lookupPath(payload, path); ok {
			if s, ok := v.(string); ok {
				if strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
	}
	return ""
}

func firstMessageCaption(payload webhookEnvelope) string {
	for _, path := range [][]string{
		{"message", "imageMessage", "caption"},
		{"data", "message", "imageMessage", "caption"},
		{"message", "videoMessage", "caption"},
		{"data", "message", "videoMessage", "caption"},
		{"message", "documentMessage", "caption"},
		{"data", "message", "documentMessage", "caption"},
	} {
		if v, ok := lookupPath(payload, path); ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func firstMessageInteractiveText(payload webhookEnvelope) string {
	for _, path := range [][]string{
		{"message", "interactiveResponseMessage", "body", "text"},
		{"data", "message", "interactiveResponseMessage", "body", "text"},
		{"message", "interactiveResponseMessage", "nativeFlowResponseMessage", "paramsJson"},
		{"data", "message", "interactiveResponseMessage", "nativeFlowResponseMessage", "paramsJson"},
		{"message", "templateMessage", "hydratedTemplate", "hydratedContentText"},
		{"data", "message", "templateMessage", "hydratedTemplate", "hydratedContentText"},
		{"message", "buttonsResponseMessage", "selectedButtonId"},
		{"data", "message", "buttonsResponseMessage", "selectedButtonId"},
		{"message", "buttonsResponseMessage", "selectedDisplayText"},
		{"data", "message", "buttonsResponseMessage", "selectedDisplayText"},
		{"message", "listResponseMessage", "title"},
		{"data", "message", "listResponseMessage", "title"},
		{"message", "listResponseMessage", "singleSelectReply", "selectedRowId"},
		{"data", "message", "listResponseMessage", "singleSelectReply", "selectedRowId"},
		{"message", "listResponseMessage", "singleSelectReply", "selectedDisplayText"},
		{"data", "message", "listResponseMessage", "singleSelectReply", "selectedDisplayText"},
		{"message", "conversation"},
		{"data", "message", "conversation"},
	} {
		if v, ok := lookupPath(payload, path); ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func deriveDirection(record MessageRecord) string {
	if record.FromMe {
		return "outbound"
	}
	if strings.EqualFold(record.EventType, "SEND_MESSAGE") {
		return "outbound"
	}
	if record.ChatJID != "" && record.SenderJID != "" && !strings.EqualFold(record.ChatJID, record.SenderJID) {
		lowerChat := strings.ToLower(record.ChatJID)
		if !strings.HasSuffix(lowerChat, "@g.us") && lowerChat != "status@broadcast" {
			return "outbound"
		}
	}
	if strings.EqualFold(record.EventType, "MESSAGE") || strings.Contains(strings.ToLower(record.EventType), "message") {
		return "inbound"
	}
	if record.ProviderMessageID == "" {
		return "inbound"
	}
	return "inbound"
}

func firstString(v any, keys ...string) string {
	for _, key := range keys {
		if found, ok := findRecursive(v, key); ok {
			if s, ok := stringValue(found); ok {
				return s
			}
		}
	}
	return ""
}

func firstBool(v any, keys ...string) bool {
	for _, key := range keys {
		if found, ok := findRecursive(v, key); ok {
			if b, ok := boolValue(found); ok {
				return b
			}
		}
	}
	return false
}

func firstAny(v any, keys ...string) any {
	for _, key := range keys {
		if found, ok := findRecursive(v, key); ok {
			return found
		}
	}
	return nil
}

func messageNode(payload webhookEnvelope) (map[string]any, bool) {
	for _, path := range [][]string{{"data", "message"}, {"message"}} {
		if v, ok := lookupPath(payload, path); ok {
			if m, ok := v.(map[string]any); ok {
				return m, true
			}
		}
	}
	return nil, false
}

func chooseFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fingerprintFromPayload(record MessageRecord, raw []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(strings.Join([]string{
		record.InstanceName,
		record.EventType,
		record.ProviderMessageID,
		record.ChatJID,
		record.SenderJID,
		record.MessageText,
		record.MessageType,
		record.Direction,
	}, "|")))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func hasKeyRecursive(v any, wanted string) bool {
	_, ok := findRecursive(v, wanted)
	return ok
}

func findRecursive(v any, wanted string) (any, bool) {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			if strings.EqualFold(k, wanted) {
				return child, true
			}
			if found, ok := findRecursive(child, wanted); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range node {
			if found, ok := findRecursive(child, wanted); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func lookupPath(v any, path []string) (any, bool) {
	if len(path) == 0 {
		return v, true
	}
	switch node := v.(type) {
	case map[string]any:
		child, ok := node[path[0]]
		if !ok {
			for k, candidate := range node {
				if strings.EqualFold(k, path[0]) {
					return lookupPath(candidate, path[1:])
				}
			}
			return nil, false
		}
		return lookupPath(child, path[1:])
	case []any:
		for _, child := range node {
			if found, ok := lookupPath(child, path); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func stringValue(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), true
		}
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	case nil:
		return "", false
	default:
		return fmt.Sprint(x), true
	}
}

func boolValue(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes", "y", "on":
			return true, true
		case "false", "0", "no", "n", "off":
			return false, true
		}
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i != 0, true
		}
	}
	return false, false
}

func parseAnyTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case string:
		if x == "" {
			return time.Time{}, errors.New("empty time string")
		}
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t.UTC(), nil
		}
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t.UTC(), nil
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return unixToTime(n), nil
		}
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return unixFloatToTime(f), nil
		}
		return time.Time{}, fmt.Errorf("unsupported time string: %q", x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return unixToTime(i), nil
		}
		if f, err := x.Float64(); err == nil {
			return unixFloatToTime(f), nil
		}
		return time.Time{}, fmt.Errorf("unsupported number time: %v", x)
	case float64:
		return unixFloatToTime(x), nil
	case int64:
		return unixToTime(x), nil
	case int:
		return unixToTime(int64(x)), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", v)
	}
}

func unixToTime(n int64) time.Time {
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}

func sanitizeWebhookPayload(raw []byte) ([]byte, error) {
	var node any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&node); err != nil {
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

func unixFloatToTime(n float64) time.Time {
	if n > 1_000_000_000_000 {
		ms := int64(n)
		return time.UnixMilli(ms).UTC()
	}
	return time.Unix(int64(n), int64((n-float64(int64(n)))*1e9)).UTC()
}
