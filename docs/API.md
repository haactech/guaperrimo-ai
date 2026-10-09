# API HTTP

Base local: `http://<host>:8080`. Si `API_KEY` está configurada, todas las
rutas menos `/health` exigen la cabecera `X-API-Key`.

## `GET /health`

`{"status":"ok"}`

## `POST /session/{id}/image`

Multipart con campo `image` (JPEG, PNG o WebP, máximo 10 MB).

```json
{"url": "https://<r2-public>/sessions/{id}/welcome_1700000000.jpg", "session_id": "{id}"}
```

## `POST /session/{id}/chat`

Request:

```json
{
  "type": "image | button_response | voice_response | text",
  "image_url": "opcional, informativo",
  "option_id": "requerido con button_response",
  "transcript": "requerido con voice_response",
  "text": "requerido con text",
  "location": {"lat": 19.4194, "lng": -99.1616, "label": "Roma Norte"},
  "radius_m": 1500,
  "allow_shipping": true
}
```

`location`, `radius_m` y `allow_shipping` son opcionales en cualquier turno.
El primer turno debe ser `type=image` (la foto se lee de R2, no del `image_url`).

Response:

```json
{
  "session_id": "…",
  "phase": "chat | done",
  "turn": 2,
  "message": "texto para TTS",
  "input_mode": "buttons | voice | none",
  "options": [{"id": "boda", "label": "Boda de día"}],
  "is_final": false,
  "location_known": true,
  "priority_actions": [{"id": "camisa_lino", "title": "…", "description": "…", "impact": "alto", "effort": "bajo", "product_ids": ["p_…"]}],
  "shopping_list": [{"slot": "upper_body", "description": "…", "why": "…", "priority": 1, "products": [Product]}],
  "products": [Product],
  "stores": [Store],
  "total_mxn": 1448,
  "looks_generating": true
}
```

`priority_actions`, `shopping_list`, `products`, `stores` y `total_mxn` solo
aparecen cuando `is_final` es `true`. Después de una recomendación el usuario
puede seguir hablando; el agente ajusta y puede volver a entregar `is_final`.

Errores: `400` cuerpo o id inválido, `404` sesión inexistente, `409` turno de
imagen repetido o turno sin foto, `502` fallo del modelo o del análisis,
`504` timeout del turno.

### Product

```json
{
  "id": "p_3f9a…", "title": "Camisa de lino azul marino", "store": "Zara MX",
  "price": 899, "currency": "MXN", "link": "https://…", "thumbnail": "https://…",
  "delivery": "Envío gratis", "rating": 4.3, "reviews": 120, "tags": ["Nearby, 2 km"],
  "nearby_store": {"place_id": "…", "name": "Zara Parque Delta", "distance_m": 640}
}
```

### Store

```json
{
  "place_id": "…", "name": "Zara Parque Delta", "address": "Av. Cuauhtémoc 462",
  "lat": 19.404, "lng": -99.156, "distance_m": 640, "rating": 4.1, "reviews": 2000,
  "open_state": "Abierto", "website": "https://…", "phone": "…", "category": "Tienda de ropa"
}
```

## `GET /session/{id}/recommendation`

Misma forma que la respuesta final de `/chat`. `404` si aún no hay recomendación.

## `GET /session/{id}/looks`

```json
{
  "session_id": "…",
  "status": "none | pending | generating | partial | ready",
  "all_ready": false, "ready_count": 1, "total_count": 3,
  "looks": [{"id": "look_1", "name": "…", "description": "…", "vibe": "…",
             "pieces": [{"slot": "upper_body", "category": "upper_body", "description": "…",
                         "product_id": "p_…", "product_name": "…", "product_image_url": "…",
                         "product_link": "…", "price": 899, "store": "Zara MX"}]}],
  "look_results": [{"look_id": "look_1", "status": "ready",
                    "pieces": [{"slot": "upper_body", "category": "upper_body", "product_id": "p_…",
                                "product_name": "…", "product_image_url": "…", "tryon_image_url": "…",
                                "generation_time_ms": 21000}],
                    "final_image_url": "…", "generation_time_ms": 43000}]
}
```

`status` es `none` cuando el try-on está deshabilitado o aún no se lanzó.

## `POST /session/{id}/tryon`

Solo existe si `GCP_PROJECT_ID` está configurado.

```json
{"product_id": "p_…"}
```

o, para compatibilidad con la app actual, `{"action_id": "camisa_lino", "garment_description": "…"}`
(se usa el primer producto con imagen de esa acción).

```json
{
  "session_id": "…", "action_id": "camisa_lino", "product_id": "p_…",
  "tryon_image_url": "https://…",
  "garment_used": {"name": "…", "source": "Zara MX", "catalog_id": "p_…", "image_url": "…", "link": "…"},
  "generation_time_ms": 24000
}
```

Errores: `404` producto no encontrado, `409` sin recomendación o sin foto,
`422` producto sin imagen, `502` descarga o generación fallida, `504` timeout.

## Probador mix & match

Disponible después de la recomendación. La cuadrícula existe aunque el try-on
esté apagado; en ese caso `tryon_available` es `false` y solo se ven las fotos
de producto.

### `GET /session/{id}/matrix`

```json
{
  "session_id": "…",
  "tryon_available": true,
  "base_image_url": "https://…/welcome_….jpg",
  "slots": [
    {"slot": "upper_body", "label": "Arriba", "options": [{ …Product, "why": "alarga el torso", "priority": 1 }]},
    {"slot": "lower_body", "label": "Abajo", "options": [ … ]},
    {"slot": "footwear",   "label": "Calzado", "options": [ … ]}
  ],
  "default": {"upper_body": "p_…", "lower_body": "p_…", "footwear": "p_…"},
  "renders": [{"key": "p_a|p_b|p_c", "selection": {…}, "status": "ready", "image_url": "https://…", "generation_time_ms": 21000}],
  "saved_looks": [SavedLook]
}
```

Las filas van en orden de capas: `upper_body`, `outerwear`, `lower_body`,
`footwear`. `renders` solo lista combinaciones completas; el servidor cachea
también los prefijos, por eso cambiar una fila suele costar una sola imagen.
Al terminar la recomendación se pre-generan la combinación por defecto y todas
las que están a un swipe de distancia.

### `POST /session/{id}/matrix/render`

```json
{"selection": {"upper_body": "p_…", "lower_body": "p_…", "footwear": "p_…"}}
```

Las filas que falten toman el valor por defecto. Respuestas:

- `200` con `status: "ready"` e `image_url` si ya existe.
- `202` con `status: "pending" | "generating"` si se está generando. Al pedirla,
  el servidor también encola las combinaciones vecinas. La app repite el POST
  o consulta `GET /matrix` cada 2 o 3 segundos y muestra shimmer mientras tanto.
- `400` selección inválida, `404` sin cuadrícula, `409` sin foto, `503` try-on apagado.

Una imagen tarda entre 15 y 45 s con Vertex. Las combinaciones ya calculadas
regresan al instante.

### `GET /session/{id}/saved-looks` y `POST /session/{id}/saved-looks`

Guardan la combinación elegida con su explicación para ir de compras.

```json
{"selection": {"upper_body": "p_…", "lower_body": "p_…", "footwear": "p_…"}, "note": "para la boda del sábado"}
```

```json
{
  "id": "saved_…", "key": "p_a|p_b|p_c", "selection": {…}, "image_url": "https://…",
  "items": [{"slot": "upper_body", "product": Product, "why": "alarga el torso"}],
  "total_mxn": 1448,
  "stores": [Store],
  "note": "…", "created_at": "…"
}
```

`items[].product` incluye `link` al comercio y `availability` cuando el
servidor resolvió el detalle del producto, y `nearby_store` si hay sucursal
cerca. Guardar la misma combinación dos veces la reemplaza.
