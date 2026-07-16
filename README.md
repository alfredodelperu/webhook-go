# webhook-go

Webhook en Go para recibir eventos de Evolution/WhatsApp y guardarlos en la base de datos local de Supabase.

## Qué hace

- Expone `POST /webhook`
- Expone `GET /health`
- Extrae campos útiles del payload:
  - `provider_message_id`
  - `event_type`
  - `message_type`
  - `from_number`
  - `sender_name`
  - `message_text`
  - `message_timestamp`
- Guarda el JSON completo en `raw_payload`

## Requisitos

- Go 1.22+
- PostgreSQL/Supabase local corriendo en esta misma máquina

## Configuración

Copia `.env.example` y ajusta:

```bash
PORT=8080
DATABASE_URL=postgres://postgres:postgres@127.0.0.1:54322/postgres?sslmode=disable
```

## Tabla

La tabla se crea automáticamente al iniciar, o puedes aplicar `schema.sql` manualmente.

## Ejecutar

### Opción 1: local

```bash
go run .
```

### Opción 2: Docker

```bash
docker build -t webhook-go .
docker run --rm --network host -e DATABASE_URL="$DATABASE_URL" -e PORT=8080 webhook-go
```

## Probar con curl

```bash
curl -X POST http://127.0.0.1:8080/webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "event":"messages.upsert",
    "data":{
      "key":{"id":"ABCD123","remoteJid":"51999999999@s.whatsapp.net"},
      "pushName":"Felix",
      "message":{"conversation":"Hola, quiero precio"},
      "messageTimestamp":1710000000
    }
  }'
```

## Prueba en Supabase local

En la base de datos deberías ver registros en `whatsapp_messages`.
