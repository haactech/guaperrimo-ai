# Migraciones PostgreSQL para StyleRAG

## Problema
Definir esquemas de base de datos para el sistema de recomendación de moda B2B, con migraciones mantenibles sin usar la convención `V1`, `V2`, `V3`.

## Solución
Flyway con **timestamps como versión**: `V20260216120000__descripcion.sql`

## Tablas

| Tabla | Propósito |
|-------|-----------|
| `retailers` | Clientes B2B (config, API keys) |
| `products` | Catálogo (metadata, el embedding va en Qdrant) |
| `sessions` | Registro de sesiones (el perfil de estilo va en Redis) |
| `session_events` | Eventos para analytics |
| `recommendations` | Productos recomendados + conversiones |

## Comandos
```bash
flyway -configFiles=flyway.conf migrate   # Aplicar
flyway -configFiles=flyway.conf info      # Estado
```

## Estructura
```
db/migrations/
├── V20260216120000__create_retailers.sql
├── V20260216120001__create_products.sql
├── V20260216120002__create_sessions.sql
├── V20260216120003__create_session_events.sql
├── V20260216120004__create_recommendations.sql
└── V20260216120005__create_updated_at_trigger.sql
```
