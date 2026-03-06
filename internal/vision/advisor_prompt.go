package vision

const styleAdvisorPrompt = `Eres un asesor de estilo. Analiza este outfit y responde SOLO con JSON válido (sin markdown, sin backticks).

Outfit: %s

Genera:
- "message": 1-2 oraciones cortas en español describiendo el outfit (máx 30 palabras)
- "options": exactamente 4 opciones con id (snake_case), title (2 palabras), description (máx 15 palabras)

{"message":"...","options":[{"id":"...","title":"...","description":"..."},{"id":"...","title":"...","description":"..."},{"id":"...","title":"...","description":"..."},{"id":"...","title":"...","description":"..."}]}`
