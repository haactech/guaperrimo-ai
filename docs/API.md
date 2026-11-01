# API HTTP

Base local: `http://<host>:8080`. Si `API_KEY` está configurada, todas las
rutas menos `/health` exigen la cabecera `X-API-Key`.

## `GET /health`

`{"status":"ok"}`

## `POST /session/{id}/image`

Multipart con campo `image` (JPEG, PNG o WebP, máximo 10 MB).

Los JPEG se guardan derechos y sin metadatos: el servidor aplica la rotación
EXIF que pone el iPhone a las fotos verticales y borra el EXIF (incluido el
GPS). Si el JPEG no se puede decodificar, se guarda tal cual.

```json
{"url": "https://<r2-public>/sessions/{id}/welcome_1700000000.jpg", "session_id": "{id}"}
```

## `POST /session/{id}/chat`

Request:

```json
{
  "type": "image | button_response | voice_response | text | idea_selection",
  "image_url": "opcional, informativo",
  "option_id": "requerido con button_response",
  "transcript": "requerido con voice_response",
  "text": "requerido con text; comentario opcional con idea_selection",
  "selected_ideas": ["idea_1", "idea_3"],
  "location": {"lat": 19.4194, "lng": -99.1616, "label": "Roma Norte"},
  "radius_m": 1500,
  "allow_shipping": true
}
```

`location`, `radius_m` y `allow_shipping` son opcionales en cualquier turno.
El primer turno debe ser `type=image` (la foto se lee de R2, no del `image_url`).

### Preguntas del servidor

Dos preguntas no las decide el modelo: las hace el servidor de forma determinista y resuelve la respuesta sin pasar por el LLM.

1. **Ubicación.** Si la app no envió `location` (del dispositivo) y el usuario no se ha negado, justo antes de la primera búsqueda de productos o del preview de estilo el servidor pregunta "¿en qué colonia o zona y ciudad estás?" (`input_mode: voice`), geocodifica la respuesta y busca tiendas cercanas. Tras dos respuestas que no se pueden ubicar, o si el usuario dice que prefiere comprar en línea, se asume envío a domicilio y el flujo continúa.
2. **Tienda preferida.** Con la ubicación conocida y al menos dos tiendas cercanas, el servidor ofrece las tres más cercanas como botones (`"Liverpool Insurgentes · 450 m"`, id = `place_id`) más `any_store` ("Cualquiera"); el agente dirige `search_products` a la tienda elegida.

Estas preguntas no cuentan para el límite de preguntas del agente. Enviar `location` desde el primer turno evita la primera; las tiendas cercanas se buscan en paralelo con el análisis de la foto y llegan en `stores` cuando `is_final` es `true`.

Response:

```json
{
  "session_id": "…",
  "phase": "chat | preview | done",
  "turn": 2,
  "message": "texto para TTS",
  "input_mode": "buttons | voice | preview | none",
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
imagen repetido, turno sin foto o `idea_selection` sin preview pendiente,
`502` fallo del modelo o del análisis, `504` timeout del turno.

### Preview de estilo (`AGENT_PREVIEW=true`)

Antes de buscar productos, cuando ya sabe ocasión y presupuesto, el agente
responde con `input_mode: "preview"`, `phase: "preview"` y un `direction`:

```json
"direction": {
  "brief": {
    "summary": "Por tu tono Deep Autumn…",
    "color_season": "Deep Autumn",
    "palette": [{"name": "Verde botella", "hex": "#3E5641"}],
    "avoid": [{"name": "Negro puro", "hex": "#000000", "why": "apaga tu tono cálido"}],
    "silhouette": "Cortes rectos, nunca ajustados"
  },
  "ideas": [{
    "id": "idea_1", "slot": "upper_body",
    "garment": "Camisa de lino manga larga",
    "colors": [{"name": "Verde botella", "hex": "#3E5641"}],
    "textures": ["lino"], "fit": "recta, sin ajustar",
    "why": "fresca y elegante para día",
    "price_min_mxn": 700, "price_max_mxn": 1200,
    "reference": Product
  }],
  "round": 1,
  "created_at": "…"
}
```

- 9 ideas, 3 por prenda del outfit y en este orden: `upper_body` (camisas,
  `idea_1`–`idea_3`), `lower_body` (pantalones, `idea_4`–`idea_6`) y
  `outerwear` (chamarras o sacos, `idea_7`–`idea_9`). Nunca accesorios ni
  calzado. Si el modelo propone menos de 2 de alguna prenda, el servidor se lo
  regresa.
- `hex` puede faltar si el modelo dio uno inválido; el nombre siempre está.
- `reference` es un producto real en pesos buscado por el servidor (una
  búsqueda por idea, en paralelo), elegido para que su título coincida con el
  color de la idea. Es solo de referencia: el producto final puede ser otro.
  Puede faltar si la búsqueda no encontró nada.

La app responde con `{"type": "idea_selection", "selected_ideas": [...],
"text": "comentario opcional"}` (al menos uno de los dos). Las ideas no
elegidas quedan como rechazadas en la sesión (`direction.selected`,
`direction.rejected`, `direction.feedback`): es la señal de qué le atrae. El
agente busca solo las elegidas y arma 2-3 looks, cada uno un outfit completo
(camisa + pantalón + chamarra); si en una prenda no eligió nada, el agente
completa con una neutra de su paleta. Si no eligió ninguna, propone otra ronda.
Mientras no haya elección, `search_products` está bloqueada.

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

Con `VTON_PROVIDER=gemini` cada look es una sola imagen con todas las prendas,
pantalón incluido, y solo la última pieza trae `tryon_image_url` (igual a
`final_image_url`). Con proveedores encadenados cada pieza trae la imagen de
su paso y el pantalón no se renderiza.

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
  "garment_used": "Camisa de lino azul marino",
  "garment": {"name": "…", "source": "Zara MX", "catalog_id": "p_…", "image_url": "…", "link": "…"},
  "generation_time_ms": 24000
}
```

`garment_used` es el nombre de la prenda (la app lo decodifica como texto);
`garment` trae el detalle completo.

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
    {"slot": "outerwear",  "label": "Encima", "options": [ … ]}
  ],
  "default": {"upper_body": "p_…", "outerwear": "p_…"},
  "renders": [{"key": "p_a|p_b", "selection": {…}, "status": "ready", "image_url": "https://…", "generation_time_ms": 10000}],
  "bottoms": {"slot": "lower_body", "label": "Abajo", "options": [ … ]},
  "complete_renders": [{"key": "p_a|p_b|p_c", "selection": {…, "lower_body": "p_c"}, "status": "ready", "image_url": "https://…"}],
  "saved_looks": [SavedLook]
}
```

Las filas van en orden de capas: `upper_body` y luego `outerwear`. El calzado
no se renderiza, y el pantalón tampoco en la cuadrícula: solo en el look
completo (abajo). `renders` lista solo combinaciones de la cuadrícula. Con
Gemini cada combinación es una imagen generada desde la foto original; con
proveedores encadenados el servidor cachea también los prefijos, por eso
cambiar una fila suele costar una sola imagen. No se pre-genera nada: cada
imagen se cobra.

`bottoms` y `complete_renders` aparecen solo cuando el look completo está
activo (`VTON_PROVIDER=gemini`, o `TRYON_LOWER_PROVIDER` con un proveedor
encadenado) y la recomendación trae pantalones con foto.

### `POST /session/{id}/matrix/render`

```json
{"selection": {"upper_body": "p_…", "outerwear": "p_…"}}
```

Las filas que falten toman el valor por defecto. Respuestas:

- `200` con `status: "ready"` e `image_url` si ya existe.
- `202` con `status: "pending" | "generating"` si se está generando. La app
  consulta `GET /matrix` cada 2 o 3 segundos y muestra shimmer mientras tanto.
  Repetir el POST sobre una combinación fallida la vuelve a intentar.
- `400` selección inválida, `404` sin cuadrícula, `409` sin foto, `503` try-on apagado.

Una imagen tarda 15–23 s con Gemini Pro (unos 10 s por prenda con FASHN). Las
combinaciones ya calculadas regresan al instante.

### `POST /session/{id}/matrix/complete`

El look completo: la combinación de la cuadrícula más el pantalón.

- Con `VTON_PROVIDER=gemini`, Gemini genera las tres prendas en una sola
  imagen desde la foto original, con el tope normal (`TRYON_MAX_PER_*`).
- Con un proveedor encadenado, el pantalón se pone al final sobre la imagen
  de la cuadrícula ya cacheada, con el proveedor de `TRYON_LOWER_PROVIDER`
  (Vertex, que sí cambia el pantalón en fotos de medio muslo). Cuesta una sola
  imagen de ese proveedor, con su propio tope.

```json
{"selection": {"upper_body": "p_…", "outerwear": "p_…"}, "lower_body": "p_…"}
```

`lower_body` es opcional (por defecto la primera opción de `bottoms`). Mismas
respuestas que `/matrix/render`; el resultado aparece en `complete_renders`.
Al pasar el tope, el render queda `failed` con un `error` que dice "límite…".
`503` si el look completo no está activo.

### `GET /session/{id}/saved-looks` y `POST /session/{id}/saved-looks`

Guardan la combinación elegida con su explicación para ir de compras.

```json
{"selection": {"upper_body": "p_…", "outerwear": "p_…", "lower_body": "p_…"}, "note": "para la boda del sábado"}
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
