package vision

const diagnosisPrompt = `Eres un asesor de imagen experto. Genera un diagnóstico estructurado
comparando el estado actual del outfit con los deseos del usuario.

Análisis del outfit:
%s

Respuestas del usuario:
%s

Genera un diagnóstico con:
1. current_assessment: fortalezas reales (no inventadas), gaps concretos,
   y distancia de estilo (baja/media/alta).
2. user_profile: resumen estructurado de lo que quiere.
3. gap_ranking: áreas a mejorar ordenadas por prioridad según lo que el
   usuario dijo que quiere. Cada área con score actual (1-10) y target (1-10).

Sé honesto. Si el outfit tiene problemas claros, nómbralos.
No inventes fortalezas para ser amable.

Responde SOLO en JSON con esta estructura:
{
  "current_assessment": {
    "strengths": ["..."],
    "gaps": ["..."],
    "style_distance": "baja|media|alta"
  },
  "user_profile": {
    "occasion": "...",
    "desired_projection": "...",
    "approach": "refinar|explorar",
    "pain_points": ["..."],
    "aspirational_reference": "...",
    "constraints": ["..."]
  },
  "gap_ranking": [
    {
      "area": "...",
      "priority": 1,
      "current": 5,
      "target": 8,
      "note": "..."
    }
  ]
}`
