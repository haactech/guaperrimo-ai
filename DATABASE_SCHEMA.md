# StyleRAG - Database Schema Documentation

## Overview

StyleRAG uses a polyglot persistence strategy with three data stores:

| Store      | Purpose                                      | Data Types                          |
|------------|----------------------------------------------|-------------------------------------|
| PostgreSQL | Relational data, config, metrics             | Products, retailers, sessions, analytics |
| Redis      | Session state with TTL                       | Style profiles, conversation context |
| Qdrant     | Vector similarity search                     | Product embeddings                  |

This document focuses on the **PostgreSQL schema**. Redis stores JSON documents (see `internal/session/manager.go`), and Qdrant stores vectors managed via its API.

---

## Migration Strategy

We use **Flyway** with timestamp-based versioning for simplicity:

```
db/migrations/
├── 20260216120000__create_retailers.sql
├── 20260216120001__create_products.sql
├── 20260216120002__create_sessions.sql
└── ...
```

**Convention:**
- Prefix: `V` (Flyway versioned migrations)
- Version: `YYYYMMDDHHMMSS` (timestamp) - avoids version conflicts
- Separator: `__` (double underscore)
- Name: `snake_case` description

**Benefits over sequential numbering (V1, V2...):**
- No merge conflicts between developers
- Clear creation timestamp
- Natural ordering

---

## Entity Relationship Diagram

```
┌─────────────┐       ┌──────────────┐       ┌─────────────────────┐
│  retailers  │       │   products   │       │      sessions       │
├─────────────┤       ├──────────────┤       ├─────────────────────┤
│ id (PK)     │──┐    │ id (PK)      │       │ id (PK)             │
│ name        │  │    │ retailer_id  │──┐    │ retailer_id (FK)    │──┐
│ slug        │  └───<│ sku          │  │    │ external_session_id │  │
│ config      │       │ name         │  │    │ user_agent          │  │
│ ...         │       │ ...          │  │    │ started_at          │  │
└─────────────┘       └──────────────┘  │    │ ended_at            │  │
                                        │    └─────────────────────┘  │
                                        │              │              │
                                        │              ▼              │
                                        │    ┌─────────────────────┐  │
                                        │    │   session_events    │  │
                                        │    ├─────────────────────┤  │
                                        │    │ id (PK)             │  │
                                        │    │ session_id (FK)     │──┘
                                        │    │ event_type          │
                                        │    │ payload             │
                                        │    │ created_at          │
                                        │    └─────────────────────┘
                                        │
                                        │    ┌─────────────────────┐
                                        │    │  recommendations    │
                                        │    ├─────────────────────┤
                                        │    │ id (PK)             │
                                        │    │ session_id (FK)     │
                                        │    │ product_id (FK)     │──┘
                                        │    │ rank                │
                                        │    │ score               │
                                        │    │ ...                 │
                                        │    └─────────────────────┘
```

---

## Tables

### 1. `retailers`

Stores B2B customer configuration.

| Column          | Type         | Constraints              | Description                          |
|-----------------|--------------|--------------------------|--------------------------------------|
| `id`            | UUID         | PK, DEFAULT gen_random_uuid() | Primary key                      |
| `name`          | VARCHAR(255) | NOT NULL                 | Company name                         |
| `slug`          | VARCHAR(100) | NOT NULL, UNIQUE         | URL-friendly identifier              |
| `api_key_hash`  | VARCHAR(255) | NOT NULL                 | Hashed API key for authentication    |
| `config`        | JSONB        | NOT NULL DEFAULT '{}'    | Retailer-specific settings           |
| `is_active`     | BOOLEAN      | NOT NULL DEFAULT true    | Soft disable                         |
| `created_at`    | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Creation timestamp                   |
| `updated_at`    | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Last update timestamp                |

**Config JSONB structure:**
```json
{
  "currency": "MXN",
  "language": "es",
  "style_focus": ["smart_casual", "casual"],
  "budget_tiers": {
    "low": 1500,
    "mid": 3000,
    "high": 6000
  },
  "features": {
    "voice_enabled": true,
    "image_analysis_enabled": true
  }
}
```

---

### 2. `products`

Product catalog metadata. Embeddings stored separately in Qdrant.

| Column          | Type         | Constraints              | Description                          |
|-----------------|--------------|--------------------------|--------------------------------------|
| `id`            | UUID         | PK, DEFAULT gen_random_uuid() | Primary key                      |
| `retailer_id`   | UUID         | FK retailers(id), NOT NULL | Owner retailer                     |
| `sku`           | VARCHAR(100) | NOT NULL                 | Retailer's product SKU               |
| `name`          | VARCHAR(255) | NOT NULL                 | Product name                         |
| `description`   | TEXT         |                          | Full description                     |
| `category`      | VARCHAR(100) | NOT NULL                 | Main category (tops, bottoms, etc.)  |
| `subcategory`   | VARCHAR(100) |                          | Subcategory (shirt, chino, etc.)     |
| `colors`        | TEXT[]       | NOT NULL DEFAULT '{}'    | Array of color names                 |
| `fit`           | VARCHAR(50)  |                          | Fit type (slim, regular, relaxed)    |
| `style_tags`    | TEXT[]       | NOT NULL DEFAULT '{}'    | Style tags for filtering             |
| `price`         | DECIMAL(10,2)| NOT NULL                 | Price in retailer's currency         |
| `sizes`         | TEXT[]       | NOT NULL DEFAULT '{}'    | Available sizes                      |
| `image_url`     | TEXT         |                          | Primary product image URL            |
| `metadata`      | JSONB        | DEFAULT '{}'             | Additional attributes                |
| `is_active`     | BOOLEAN      | NOT NULL DEFAULT true    | Product availability                 |
| `created_at`    | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Creation timestamp                   |
| `updated_at`    | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Last update timestamp                |

**Indexes:**
- `(retailer_id, sku)` UNIQUE - Ensure SKU uniqueness per retailer
- `(retailer_id, category)` - Filter by category
- `(retailer_id, is_active)` - Active products query
- GIN on `style_tags` - Tag-based filtering
- GIN on `colors` - Color filtering

---

### 3. `sessions`

Session records for analytics. Actual style profile stored in Redis.

| Column               | Type         | Constraints              | Description                       |
|----------------------|--------------|--------------------------|-----------------------------------|
| `id`                 | UUID         | PK, DEFAULT gen_random_uuid() | Primary key                  |
| `retailer_id`        | UUID         | FK retailers(id), NOT NULL | Session's retailer             |
| `external_session_id`| VARCHAR(255) |                          | Client-provided session ID        |
| `user_agent`         | TEXT         |                          | Client user agent                 |
| `ip_hash`            | VARCHAR(64)  |                          | Hashed IP for analytics           |
| `started_at`         | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Session start                     |
| `ended_at`           | TIMESTAMPTZ  |                          | Session end (null if active)      |
| `turn_count`         | INT          | NOT NULL DEFAULT 0       | Number of conversation turns      |
| `has_image_upload`   | BOOLEAN      | NOT NULL DEFAULT false   | Whether user uploaded an image    |
| `has_purchase`       | BOOLEAN      | NOT NULL DEFAULT false   | Whether session led to purchase   |
| `total_tokens_used`  | INT          | NOT NULL DEFAULT 0       | LLM tokens consumed               |

**Indexes:**
- `(retailer_id, started_at)` - Session queries by retailer
- `(retailer_id, has_purchase)` - Conversion analysis

---

### 4. `session_events`

Event log for detailed analytics.

| Column       | Type         | Constraints              | Description                          |
|--------------|--------------|--------------------------|--------------------------------------|
| `id`         | UUID         | PK, DEFAULT gen_random_uuid() | Primary key                     |
| `session_id` | UUID         | FK sessions(id), NOT NULL| Parent session                       |
| `event_type` | VARCHAR(50)  | NOT NULL                 | Event type (see enum below)          |
| `payload`    | JSONB        | DEFAULT '{}'             | Event-specific data                  |
| `created_at` | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Event timestamp                      |

**Event types:**
- `message_user` - User sent a message
- `message_agent` - Agent responded
- `image_uploaded` - User uploaded outfit photo
- `image_analyzed` - Vision analysis completed
- `search_executed` - RAG search performed
- `products_shown` - Recommendations displayed
- `product_clicked` - User clicked a product
- `product_purchased` - User completed purchase
- `voice_transcribed` - Voice input transcribed

**Indexes:**
- `(session_id, created_at)` - Event timeline
- `(event_type, created_at)` - Event type analysis

---

### 5. `recommendations`

Track which products were recommended in each session.

| Column        | Type         | Constraints              | Description                          |
|---------------|--------------|--------------------------|--------------------------------------|
| `id`          | UUID         | PK, DEFAULT gen_random_uuid() | Primary key                     |
| `session_id`  | UUID         | FK sessions(id), NOT NULL| Session that received recommendation |
| `product_id`  | UUID         | FK products(id), NOT NULL| Recommended product                  |
| `rank`        | INT          | NOT NULL                 | Position in recommendation list      |
| `score`       | DECIMAL(5,4) |                          | RAG relevance score (0-1)            |
| `context`     | JSONB        | DEFAULT '{}'             | Why this was recommended             |
| `was_clicked` | BOOLEAN      | NOT NULL DEFAULT false   | User clicked this recommendation     |
| `was_purchased`| BOOLEAN     | NOT NULL DEFAULT false   | User purchased this product          |
| `created_at`  | TIMESTAMPTZ  | NOT NULL DEFAULT NOW()   | Recommendation timestamp             |

**Context JSONB structure:**
```json
{
  "query_style_tags": ["smart_casual"],
  "matched_tags": ["smart_casual", "minimal"],
  "price_in_budget": true,
  "outfit_role": "bottom"  // top, bottom, footwear, accessory
}
```

**Indexes:**
- `(session_id, rank)` - Recommendations per session
- `(product_id, was_clicked)` - Product click-through analysis
- `(product_id, was_purchased)` - Product conversion analysis

---

## Flyway Configuration

**flyway.conf:**
```properties
flyway.url=jdbc:postgresql://localhost:5432/stylerag
flyway.user=${POSTGRES_USER}
flyway.password=${POSTGRES_PASSWORD}
flyway.locations=filesystem:db/migrations
flyway.baselineOnMigrate=true
flyway.validateMigrationNaming=true
```

**Commands:**
```bash
# Run migrations
flyway migrate

# Check status
flyway info

# Validate migrations
flyway validate

# Repair checksum issues (use with caution)
flyway repair
```

---

## Redis Data Structures

Style profiles are stored in Redis with TTL (not in PostgreSQL):

**Key pattern:** `session:{session_id}:profile`
**TTL:** 24 hours (configurable)

```json
{
  "id": "uuid",
  "current_style": ["casual básico", "streetwear"],
  "target_style": ["smart casual"],
  "color_preferences": {
    "likes": ["neutros"],
    "dislikes": ["neón"]
  },
  "fit_preference": "slim pero cómodo",
  "occasions": ["oficina remota", "cena casual"],
  "budget": {
    "min": 500,
    "max": 3000,
    "currency": "MXN"
  },
  "restrictions": ["no corbatas"],
  "transformation_level": "gradual",
  "conversation_history": [
    {"role": "user", "content": "...", "timestamp": "..."},
    {"role": "assistant", "content": "...", "timestamp": "..."}
  ],
  "created_at": "2026-02-16T12:00:00Z",
  "updated_at": "2026-02-16T12:05:00Z"
}
```

---

## Qdrant Collections

Product embeddings stored in Qdrant:

**Collection:** `products`

```json
{
  "vectors": {
    "size": 1536,  // or 512 for CLIP
    "distance": "Cosine"
  },
  "payload_schema": {
    "retailer_id": "keyword",
    "category": "keyword",
    "subcategory": "keyword",
    "style_tags": "keyword[]",
    "colors": "keyword[]",
    "price": "float",
    "fit": "keyword",
    "is_active": "bool"
  }
}
```

The `id` in Qdrant matches the `id` in PostgreSQL `products` table.
