# Evolution Go payload samples

This folder contains 10 **synthetic but parser-compatible** payload examples showing how webhook events can arrive from Evolution Go.

They are anonymized and modeled after the formats supported by `webhook-go`'s `extract.go` parser:
- `messages.upsert` events with `data.key.remoteJid`
- `Message` events with `data.Info.Chat` and `data.Info.Sender`
- text, image caption, buttons response, list response, interactive response, protocol edited message, group chat, status broadcast, and unknown-type cases

## Files
- `01-inbound-text.json`
- `02-outbound-text.json`
- `03-inbound-image-caption.json`
- `04-outbound-buttons-response.json`
- `05-outbound-protocol-edited.json`
- `06-inbound-list-response.json`
- `07-outbound-interactive-response.json`
- `08-group-inbound-text.json`
- `09-status-broadcast-event.json`
- `10-unknown-message-type.json`

These examples are safe to inspect, diff, and use as a reference for parser behavior.
