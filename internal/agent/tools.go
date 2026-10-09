package agent

import "stylerag/internal/llm"

const (
	toolUpdateProfile  = "update_profile"
	toolSetLocation    = "set_location"
	toolFindStores     = "find_nearby_stores"
	toolSearchProducts = "search_products"
	toolAskUser        = "ask_user"
	toolFinish         = "finish_recommendation"
)

// --- tiny JSON-schema helpers ---

func obj(props map[string]any, required []string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func strArr(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func objArr(item map[string]any, desc string) map[string]any {
	return map[string]any{"type": "array", "items": item, "description": desc}
}
func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}

// toolDefinitions is the full tool set exposed to the model.
func toolDefinitions() []llm.Tool {
	return []llm.Tool{
		{
			Name:        toolUpdateProfile,
			Description: "Registra datos del usuario en cuanto los sepas. Envía solo los campos nuevos o corregidos.",
			Parameters: obj(map[string]any{
				"occasion":       str("Ocasión o momento: boda, cita, entrevista, oficina, día a día..."),
				"event_date":     str("Fecha aproximada del evento (YYYY-MM-DD o texto como 'este sábado')"),
				"style_goal":     str("Cómo quiere verse o qué quiere proyectar"),
				"budget_mxn":     num("Presupuesto total aproximado en pesos mexicanos"),
				"radius_m":       integer("Distancia máxima a tiendas en metros (1500 = caminable)"),
				"allow_shipping": boolean("true si acepta comprar en línea con envío a domicilio"),
				"constraints":    strArr("Cosas que quiere evitar (colores, prendas, estilos)"),
				"pain_points":    strArr("Lo que no le gusta de cómo se ve hoy"),
				"notes":          str("Cualquier otro contexto útil"),
			}, nil),
		},
		{
			Name:        toolSetLocation,
			Description: "Convierte la zona que dijo el usuario (colonia, ciudad) en coordenadas. Úsala solo si la ubicación es desconocida.",
			Parameters: obj(map[string]any{
				"query": str("Lugar tal como lo dijo el usuario, con ciudad. Ej: 'Roma Norte, Ciudad de México'"),
			}, []string{"query"}),
		},
		{
			Name:        toolFindStores,
			Description: "Busca tiendas físicas de ropa para hombre cerca de la ubicación del usuario, ordenadas por distancia.",
			Parameters: obj(map[string]any{
				"query":    str("Tipo de tienda. Por defecto: 'tienda de ropa para hombre'"),
				"radius_m": integer("Radio en metros. Por defecto el del perfil (1500 = caminable)"),
			}, nil),
		},
		{
			Name:        toolSearchProducts,
			Description: "Busca productos reales a la venta en México. Devuelve ids de producto que DEBES usar en la recomendación.",
			Parameters: obj(map[string]any{
				"query":         str("Descripción en español con tipo de prenda, color y corte, más la palabra 'hombre'"),
				"max_price_mxn": num("Precio máximo por artículo en pesos"),
				"store":         str("Nombre de una tienda para dirigir la búsqueda (opcional)"),
				"limit":         integer("Máximo de resultados, por defecto 6"),
			}, []string{"query"}),
		},
		{
			Name:        toolAskUser,
			Description: "Termina el turno haciendo UNA pregunta al usuario.",
			Parameters: obj(map[string]any{
				"message":    str("Texto que se leerá en voz alta. Breve y natural."),
				"input_mode": enum("buttons cuando hay 2-4 opciones concretas; voice para preguntas abiertas", "buttons", "voice"),
				"options": objArr(obj(map[string]any{
					"id":    str("identificador corto en snake_case"),
					"label": str("texto del botón, máximo 4 palabras"),
				}, []string{"id", "label"}), "Opciones cuando input_mode es buttons"),
			}, []string{"message", "input_mode"}),
		},
		{
			Name:        toolFinish,
			Description: "Termina el turno entregando la recomendación final con productos reales.",
			Parameters: obj(map[string]any{
				"summary": str("Resumen para leer en voz alta: máximo 3 frases, algo positivo real, el cambio de mayor impacto y un cierre"),
				"priority_actions": objArr(obj(map[string]any{
					"id":          str("snake_case"),
					"title":       str("2-4 palabras"),
					"description": str("Una frase accionable con color y corte específicos"),
					"impact":      enum("impacto", "alto", "medio", "bajo"),
					"effort":      enum("esfuerzo", "alto", "medio", "bajo"),
					"product_ids": strArr("ids de search_products que resuelven esta acción"),
				}, []string{"id", "title", "description", "impact", "effort"}), "2-4 acciones ordenadas por impacto"),
				"shopping_list": objArr(obj(map[string]any{
					"slot":        enum("tipo de prenda", "upper_body", "lower_body", "footwear", "outerwear", "accessory"),
					"description": str("Qué comprar, en lenguaje simple"),
					"why":         str("Por qué le favorece, una frase"),
					"product_ids": strArr("1-3 ids de search_products; el primero es la opción principal"),
					"priority":    integer("1 = comprar primero"),
				}, []string{"slot", "description", "product_ids"}), "2-5 artículos"),
				"looks": objArr(obj(map[string]any{
					"name":        str("Nombre corto del look"),
					"description": str("Una frase"),
					"vibe":        str("ej. casual elevado"),
					"pieces": objArr(obj(map[string]any{
						"slot":       enum("slot", "upper_body", "lower_body", "footwear", "outerwear"),
						"product_id": str("id de search_products con imagen"),
					}, []string{"slot", "product_id"}), "prendas del look"),
				}, []string{"name", "pieces"}), "2-3 looks completos"),
			}, []string{"summary", "priority_actions", "shopping_list"}),
		},
	}
}
