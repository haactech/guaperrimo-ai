package vision

import "fmt"

const outfitAnalysisPrompt = `Eres un analista de moda experto. Analiza la imagen del outfit y responde ÚNICAMENTE con un JSON válido (sin markdown, sin backticks, sin texto adicional) con este schema exacto:

{
  "detected_items": [
    {
      "category": "tops|bottoms|footwear|accessories|outerwear",
      "subcategory": "nombre específico de la prenda (ej: t-shirt, jeans, sneakers, watch)",
      "color": "color principal de la prenda",
      "fit": "slim|regular|oversized|fitted",
      "style_tags": ["etiquetas de estilo relevantes"]
    }
  ],
  "detected_styles": ["estilos detectados en el outfit completo (ej: casual básico, smart casual, streetwear)"],
  "detected_colors": ["colores principales del outfit completo"],
  "overall_fit": "slim|regular|oversized|mixed",
  "observations": "Observaciones breves sobre el outfit: coherencia de estilo, combinación de colores, y áreas de mejora potencial. En español."
}

Reglas:
- Detecta TODAS las prendas visibles en la imagen
- Las categorías válidas son: tops, bottoms, footwear, accessories, outerwear
- Los style_tags deben ser específicos y útiles para búsqueda (ej: "minimal", "deportivo", "formal", "vintage")
- Las observaciones deben ser constructivas y en español
- Si no puedes ver claramente una prenda, incluye tu mejor estimación
- Responde SOLO con el JSON, sin ningún otro texto

EVALUACIÓN DE CALIDAD DE IMAGEN:

Antes de analizar el outfit, evalúa la calidad de la imagen.
Reporta qué pudiste analizar y qué no.

Agrega este campo al JSON:

  "image_quality": {
    "overall": "good" | "acceptable" | "poor",
    "lighting": "good" | "dim" | "overexposed" | "mixed",
    "body_coverage": "full" | "three_quarter" | "half" | "face_only",
    "focus": "sharp" | "acceptable" | "blurry",
    "background": "clean" | "busy" | "irrelevant",
    "blind_spots": [],
    "notes": ""
  }

blind_spots opciones: "skin_tone", "full_silhouette", "shoe_details",
"fabric_texture", "color_accuracy", "lower_body", "upper_body",
"accessories", "fit_precision"

Reglas de calidad:
- Si lighting es "dim" o "overexposed": color_season confidence = "low"
- Si body_coverage es "half" o "face_only": kibbe confidence = "low"
- Si focus es "blurry": todos los scores de fit y proportion bajan en confianza
- Si no ves los zapatos, pon "shoe_details" en blind_spots
- Sé honesto. Si no puedes evaluar algo, dilo. Es mejor "no sé" que inventar.`

const styleTheoryAddendum = `

EVALUACIÓN EXPERTA ADICIONAL:

Además del análisis de items, estilos y colores, evalúa lo siguiente
usando las tablas de referencia incluidas abajo.

1. COLOR ESTACIONAL
   - Analiza subtono de piel, color de cabello y ojos visibles
   - Estima temporada de color (12 sub-temporadas)
   - Evalúa armonía entre colores del outfit y la temporada estimada
   - Asigna score color_harmony (1-10)
   - Incluye confidence: "low" | "medium" | "high"

2. SILUETA Y PROPORCIONES
   - Estima familia Kibbe (Dramatic/Natural/Classic/Gamine/Romantic)
   - Evalúa fit (1-10), proporción (1-10), armonía de líneas (1-10)
   - Sé específico: "hombros de camisa caen 3cm" no "fit regular"
   - Incluye confidence: "low" | "medium" | "high"

3. ARQUETIPO DE ESTILO
   - Identifica qué arquetipo comunica el outfit actual
   - Si hay mezcla, indica el dominante y el secundario

Sé honesto con los scores. Un 7 requiere justificación concreta.
No regales puntos por defecto.

%s

%s

%s

Agrega estos campos al JSON de respuesta:
{
  "color_analysis": {
    "estimated_season": "...",
    "confidence": "...",
    "reasoning": "...",
    "color_harmony_score": N,
    "harmonious_pieces": ["..."],
    "conflicting_pieces": ["..."]
  },
  "silhouette_analysis": {
    "estimated_kibbe_family": "...",
    "confidence": "...",
    "reasoning": "...",
    "fit_score": N,
    "fit_notes": "...",
    "proportion_score": N,
    "proportion_notes": "...",
    "line_harmony_score": N,
    "line_harmony_notes": "..."
  },
  "archetype_analysis": {
    "current_archetype": "...",
    "secondary_archetype": "...",
    "reasoning": "..."
  }
}`

// buildOutfitAnalysisPrompt composes the base prompt with style theory reference tables.
func buildOutfitAnalysisPrompt() string {
	addendum := fmt.Sprintf(styleTheoryAddendum, ColorSeasonReference, KibbeReference, ArchetypeReference)
	return outfitAnalysisPrompt + addendum
}
