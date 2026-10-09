# Arquitectura — guaperrimo.ai backend (v2, agentes)

## Por qué cambió

La primera iteración era un pipeline fijo con RAG sobre un dataset académico
(Kaggle, 2019) sin precio, tienda ni stock. Eso no puede producir "artículos
cerca de ti dentro de tu presupuesto". La v2 reemplaza el RAG por un agente
con herramientas cuya única fuente de productos es búsqueda de productos reales
(Google Shopping vía SerpAPI) y tiendas físicas cercanas (Google Maps vía SerpAPI).

## Flujo de una sesión

1. **Foto.** `POST /session/{id}/image` sube la foto a R2.
2. **Análisis.** El primer `POST /session/{id}/chat` (`type=image`) descarga la
   foto y pide al modelo de visión un JSON con prendas, colores, temporada de
   color, familia Kibbe, arquetipo y calidad de la foto (`internal/vision`).
3. **Conversación.** Cada turno corre `agent.Runner.RunTurn`:
   - se reconstruye el system prompt con el resumen de la foto, el perfil,
     la ubicación conocida y cuántas preguntas van;
   - el modelo debe llamar herramientas (`tool_choice: required`);
   - Go ejecuta cada herramienta y devuelve JSON compacto;
   - el turno termina cuando el modelo llama `ask_user` o `finish_recommendation`.
4. **Recomendación.** `finish_recommendation` se valida en Go: todos los
   `product_ids` deben existir en `state.Products`; si la suma de los productos
   principales supera el presupuesto en más de 10 %, se rechaza una vez para que
   el modelo busque alternativas. Se guarda `Recommendation` y `Looks`.
5. **Looks (opcional).** Si hay `GCP_PROJECT_ID`, `LookGenerator` genera en
   segundo plano el try-on de cada look usando las miniaturas de los productos,
   encadenando upper_body → lower_body. iOS hace polling a `GET /session/{id}/looks`.

## Herramientas del agente

| Herramienta | Qué hace | Estado que toca |
|---|---|---|
| `update_profile` | registra ocasión, fecha, objetivo, presupuesto, radio, envío, restricciones | `Profile` |
| `set_location` | geocodifica "colonia, ciudad" | `Location` (fuente conversación) |
| `find_nearby_stores` | tiendas de ropa por distancia; amplía a 5 km si hay menos de 3 | `Stores`, re-anota `Products` |
| `search_products` | productos reales con precio, tienda, link, imagen; marca `nearby_store` si la tienda existe cerca | `Products` |
| `ask_user` | termina el turno con una pregunta (buttons/voice) | `Transcript` |
| `finish_recommendation` | termina el turno con resumen, acciones, lista de compras y looks | `Recommendation`, `Looks`, `Phase=done` |

La ubicación puede venir del dispositivo (`location` en cualquier turno de
chat) o de la conversación. Si viene del dispositivo, el agente no la pregunta.

## Estado de sesión

`session.State` es JSON puro: perfil, memoria del agente (`Messages`, incluye
tool calls y resultados), transcript visible, productos y tiendas vistos,
recomendación, looks y try-ons. Dos stores:

- `MemoryStore`: TTL, copias privadas en `Get`, lock por sesión en `Update`.
- `PostgresStore`: tabla `sessions(id, state jsonb, updated_at)` creada al arrancar;
  `Update` usa `SELECT ... FOR UPDATE`.

Los handlers trabajan sobre una copia y al final hacen `Update` copiando solo
los campos que el turno posee, así el generador de looks nunca pierde resultados.

## LLM

`internal/llm.OpenAICompat` implementa chat completions con tools para
cualquier API compatible (Mistral, Moonshot, OpenAI). Diferencias por sabor:
Mistral usa `tool_choice: "any"` e `image_url` como string; Moonshot omite
`temperature` y `response_format`. Reintenta 429/5xx con backoff.

Modelos por defecto: `mistral-large-latest` para chat+tools y visión.
Configurable con `LLM_MODEL` y `VISION_MODEL`.

## Seguridad y límites

- `API_KEY` opcional: exige `X-API-Key` en todo menos `/health`.
- Los ids de sesión se validan (`^[A-Za-z0-9_-]{6,80}$`).
- Descargas de imágenes: solo http(s), tope de 10 MB, timeout 15 s.
- Los errores HTTP nunca incluyen la salida cruda del modelo.
- Pendiente: las fotos en R2 siguen siendo públicas por URL; falta expiración o URLs firmadas.

## Lo que se eliminó respecto a v1

Qdrant, embeddings, catálogo Postgres, pipeline de Kaggle, orquestador muerto,
diagnóstico con scores, reglas de avance forzado, endpoint `/analyze`, Redis,
`internal/voice`, binarios commiteados y migraciones Flyway. El snapshot previo
está en la rama `pre-agents-snapshot`.

## Aprendizajes de las pruebas con SerpAPI real (2026-10-08)

- `max_price` en Google Shopping vía SerpAPI devuelve un resultado vacío
  ("Fully empty"). Nunca se manda; el precio se filtra del lado del servidor.
- Cada consulta tarda entre 1 y 25 s. Por eso las herramientas de una misma
  respuesta del modelo corren en paralelo, hay un tope de búsquedas por turno
  (`AGENT_MAX_SEARCHES`), un guardia que rechaza búsquedas cuando quedan menos
  de 25 s de turno, y una caché de 15 minutos para que un reintento sea instantáneo.
- Las listas de eBay, AliExpress y similares se descartan, igual que precios
  menores a 60 MXN, que suelen ser monedas mal interpretadas.
- Los comercios de Google Shopping rara vez coinciden con boutiques locales;
  `nearby_store` aparece sobre todo con cadenas (H&M, Zara, Liverpool). Cuando
  no hay producto, el agente puede incluir el artículo sin `product_ids` y decir
  en qué tienda cercana buscarlo.
- Un turno final típico toma entre 30 y 60 s con el modelo real. iOS espera
  hasta 180 s por turno de chat.

## Probador mix & match (2026-10-08)

El objetivo es que el usuario deslice una fila (arriba, encima, abajo,
calzado) y vea la combinación sobre su propia foto, y que pueda guardar la que
le gustó con la explicación y las tiendas.

- **Cuadrícula.** `session.BuildMatrix` la deriva de la recomendación: por slot,
  los productos con imagen de la lista de compras y de los looks, el principal
  primero, máximo `MATRIX_MAX_PER_SLOT`. Los accesorios no entran porque el
  try-on no los renderiza.
- **Render por prefijos.** `tryon.Renderer` encadena las prendas en orden de
  capas y guarda cada prefijo como un render propio en `State.Renders`. Cambiar
  los zapatos cuesta una llamada; cambiar la camisa cuesta tantas como filas.
  Las peticiones duplicadas en vuelo se colapsan, la concurrencia está acotada
  y el prefetch de vecinos nunca ocupa el último slot libre, así la petición del
  usuario no espera detrás del trabajo especulativo.
- **Prewarm.** Al terminar la recomendación se encola la combinación por
  defecto y las que están a un swipe; con 3×3×2 son unas 11 imágenes.
- **Foto de la prenda.** Antes de renderizar se piden los detalles del producto
  (`google_product` en SerpAPI): fotos de hasta 600 px, link del comercio y
  disponibilidad. Se descargan hasta tres candidatas y gana la de más píxeles.
  Cuesta una búsqueda por producto; se apaga con `RESOLVE_PRODUCT_DETAILS=false`.
- **Looks.** `LookGenerator` ahora pasa por el mismo `Renderer`, así los looks
  del agente y la cuadrícula comparten imágenes.
- **Guardado.** `POST /saved-looks` congela la selección con producto, porqué,
  total y tiendas cercanas. Vive en la sesión; con Postgres sobrevive días.
