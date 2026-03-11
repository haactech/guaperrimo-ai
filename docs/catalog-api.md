# API de Catalogo de Productos

API REST interna para consulta de productos desde PostgreSQL. Diseñada para dos consumidores principales:

1. **Motor RAG** — Hidratacion de metadata despues de busqueda vectorial en Qdrant (`POST /products/batch`)
2. **Debug/Admin** — Navegacion y filtrado del catalogo (`GET /products`, `GET /products/categories`)

> **Nota:** Sin autenticacion (PoC). En produccion se usara API key del retailer via middleware.

---

## Base URL

```
http://localhost:8080
```

---

## Recursos

### Producto

```json
{
  "id": "6377254e-2884-48df-a685-69e535401c18",
  "name": "Blue Oxford Shirt",
  "description": "Classic blue oxford button-down shirt",
  "category": "Topwear",
  "subcategory": "Shirts",
  "colors": ["Blue"],
  "fit": "Regular",
  "style_tags": ["Casual", "Smart Casual"],
  "price": 899.00,
  "sizes": ["S", "M", "L", "XL"],
  "image_url": "https://example.com/blue-oxford.jpg"
}
```

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `id` | `UUID` | Identificador unico (generado por PostgreSQL) |
| `name` | `string` | Nombre del producto |
| `description` | `string` | Descripcion detallada |
| `category` | `string` | Categoria principal (Topwear, Bottomwear, Footwear, Accessories) |
| `subcategory` | `string` | Subcategoria (Shirts, Jeans, Sneakers, etc.) |
| `colors` | `string[]` | Colores disponibles |
| `fit` | `string` | Tipo de corte (Slim, Regular, Relaxed). Vacio si no aplica |
| `style_tags` | `string[]` | Etiquetas de estilo (Casual, Formal, Smart Casual, etc.) |
| `price` | `number` | Precio en MXN |
| `sizes` | `string[]` | Tallas disponibles |
| `image_url` | `string` | URL de imagen del producto |

---

## Endpoints

### 1. Listar productos con filtros

```
GET /products
```

Retorna productos activos con filtros opcionales y paginacion.

#### Query Parameters

| Parametro | Tipo | Default | Descripcion |
|-----------|------|---------|-------------|
| `category` | `string` | — | Filtrar por categoria exacta |
| `subcategory` | `string` | — | Filtrar por subcategoria exacta |
| `colors` | `string` | — | Colores separados por coma. Usa overlap (`&&`): retorna productos que tengan **al menos uno** de los colores |
| `style_tags` | `string` | — | Tags separados por coma. Overlap: retorna productos con **al menos uno** de los tags |
| `fit` | `string` | — | Filtrar por tipo de corte exacto |
| `min_price` | `number` | — | Precio minimo (inclusive) |
| `max_price` | `number` | — | Precio maximo (inclusive) |
| `offset` | `integer` | `0` | Numero de productos a saltar |
| `limit` | `integer` | `20` | Productos por pagina (max: 100) |

#### Respuesta `200 OK`

```json
{
  "products": [
    { "id": "...", "name": "...", ... }
  ],
  "total": 44231,
  "offset": 0,
  "limit": 20
}
```

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `products` | `Product[]` | Lista de productos (siempre array, nunca `null`) |
| `total` | `integer` | Total de productos que coinciden con los filtros |
| `offset` | `integer` | Offset aplicado |
| `limit` | `integer` | Limite aplicado (puede ser menor al solicitado si > 100) |

#### Ejemplos

```bash
# Todos los productos (primeros 20)
curl 'localhost:8080/products'

# Topwear con limite
curl 'localhost:8080/products?category=Topwear&limit=5'

# Filtro por colores (overlap: Blue OR Black)
curl 'localhost:8080/products?colors=Blue,Black'

# Combinacion de filtros
curl 'localhost:8080/products?style_tags=Smart+Casual&fit=Slim&min_price=500&max_price=3000'

# Paginacion
curl 'localhost:8080/products?offset=20&limit=20'
```

---

### 2. Obtener producto por ID

```
GET /products/{id}
```

Retorna un unico producto por su UUID.

#### Path Parameters

| Parametro | Tipo | Descripcion |
|-----------|------|-------------|
| `id` | `UUID` | ID del producto |

#### Respuesta `200 OK`

```json
{
  "id": "6377254e-2884-48df-a685-69e535401c18",
  "name": "Blue Oxford Shirt",
  "description": "Classic blue oxford button-down shirt",
  "category": "Topwear",
  "subcategory": "Shirts",
  "colors": ["Blue"],
  "fit": "Regular",
  "style_tags": ["Casual", "Smart Casual"],
  "price": 899.00,
  "sizes": ["S", "M", "L", "XL"],
  "image_url": "https://example.com/blue-oxford.jpg"
}
```

#### Respuesta `404 Not Found`

```json
{"error": "product not found"}
```

Se retorna cuando el ID no existe o el producto esta inactivo (soft-deleted).

#### Ejemplo

```bash
curl 'localhost:8080/products/6377254e-2884-48df-a685-69e535401c18'
```

---

### 3. Obtener productos por lote (Batch)

```
POST /products/batch
```

Obtiene multiples productos por sus IDs en una sola peticion. **Endpoint principal para el motor RAG**: Qdrant retorna IDs por similitud vectorial, este endpoint hidrata la metadata desde PostgreSQL.

#### Request Body

```json
{
  "ids": [
    "6377254e-2884-48df-a685-69e535401c18",
    "f0d47e7c-85a3-4f98-b603-90825ab4381c",
    "00000000-0000-0000-0000-000000000000"
  ]
}
```

| Campo | Tipo | Restriccion | Descripcion |
|-------|------|-------------|-------------|
| `ids` | `UUID[]` | max 100 | Lista de IDs a buscar |

#### Respuesta `200 OK`

```json
{
  "products": [
    { "id": "6377254e-...", "name": "Blue Oxford Shirt", ... },
    { "id": "f0d47e7c-...", "name": "White Sneakers", ... }
  ]
}
```

**Comportamiento clave:**
- Retorna **solo los productos encontrados**. IDs inexistentes o inactivos se omiten silenciosamente.
- **No retorna 404** por IDs faltantes — esto es intencional. Entre la busqueda en Qdrant y la hidratacion en PostgreSQL, productos pueden haber sido soft-deleted.
- El array `products` siempre es `[]`, nunca `null`.
- El caller debe manejar respuestas parciales (puede recibir menos productos que IDs enviados).

#### Respuesta `400 Bad Request`

```json
{"error": "max 100 ids allowed"}
```

```json
{"error": "invalid request body"}
```

#### Ejemplo

```bash
curl -X POST 'localhost:8080/products/batch' \
  -H 'Content-Type: application/json' \
  -d '{"ids": ["6377254e-2884-48df-a685-69e535401c18", "f0d47e7c-85a3-4f98-b603-90825ab4381c"]}'
```

---

### 4. Listar categorias

```
GET /products/categories
```

Retorna todas las categorias con la cantidad de productos activos en cada una. Util para construir filtros en el frontend o para debug del catalogo.

#### Respuesta `200 OK`

```json
{
  "categories": [
    { "category": "Topwear", "count": 15234 },
    { "category": "Bottomwear", "count": 12087 },
    { "category": "Accessories", "count": 8901 },
    { "category": "Footwear", "count": 8009 }
  ]
}
```

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `categories` | `CategoryCount[]` | Categorias ordenadas por `count` descendente |
| `categories[].category` | `string` | Nombre de la categoria |
| `categories[].count` | `integer` | Numero de productos activos |

#### Ejemplo

```bash
curl 'localhost:8080/products/categories'
```

---

## Codigos de respuesta

| Codigo | Significado | Cuando |
|--------|-------------|--------|
| `200` | OK | Peticion exitosa |
| `400` | Bad Request | Body invalido, excede limite de IDs |
| `404` | Not Found | Producto no existe (solo `GET /products/{id}`) |
| `500` | Internal Server Error | Error de base de datos u otro error interno |

---

## Flujo RAG: Qdrant → Batch → Respuesta

```
┌──────────┐         ┌─────────┐         ┌────────────┐
│  Agent   │         │ Qdrant  │         │ PostgreSQL │
└────┬─────┘         └────┬────┘         └─────┬──────┘
     │                    │                    │
     │  vector search     │                    │
     │  (query embedding) │                    │
     │───────────────────►│                    │
     │                    │                    │
     │  IDs + scores      │                    │
     │◄───────────────────│                    │
     │                    │                    │
     │  POST /products/batch                   │
     │  {"ids": [...]}                         │
     │────────────────────────────────────────►│
     │                                         │
     │  {"products": [...]}                    │
     │◄────────────────────────────────────────│
     │                    │                    │
     │  merge scores +    │                    │
     │  metadata, rank,   │                    │
     │  present to user   │                    │
     └────────────────────┘                    │
```

---

## Notas de implementacion

- **Arrays usan overlap** (`&&`): Los filtros `colors` y `style_tags` usan el operador PostgreSQL `&&` sobre columnas `TEXT[]` con indices GIN, por lo que buscan interseccion (al menos un match).
- **Soft deletes**: Todos los queries filtran por `is_active = true`. Un `DELETE` no borra el registro, solo marca `is_active = false`.
- **Null safety**: Los handlers convierten slices `nil` a slices vacios antes de serializar a JSON, garantizando `"products": []` en vez de `"products": null`.
- **Sin paginacion en batch**: `POST /products/batch` no pagina — se limita a 100 IDs por request.
