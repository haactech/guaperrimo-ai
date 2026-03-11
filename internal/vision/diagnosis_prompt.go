package vision

const diagnosisPrompt = `Eres un asesor de imagen experto. Genera un diagnóstico estructurado
comparando el estado actual del outfit con los deseos del usuario.

Análisis del outfit (incluye evaluación de color, silueta y arquetipo):
%s

Respuestas del usuario:
%s

Perfil parcial del usuario (identidad + contexto):
%s

Genera un diagnóstico con la estructura "profile" que incluye:
1. strengths: fortalezas reales (no inventadas)
2. gaps: problemas concretos
3. style_distance: "baja" | "media" | "alta"
4. desired_archetype: inferido del contexto del usuario
5. scores: 6 dimensiones (1-10)
6. gap_analysis: áreas a mejorar ordenadas por prioridad

Sé honesto. Si el outfit tiene problemas claros, nómbralos.
No inventes fortalezas para ser amable.

Responde SOLO en JSON con esta estructura:
{
  "profile": {
    "strengths": ["..."],
    "gaps": ["..."],
    "style_distance": "baja|media|alta",
    "desired_archetype": "...",
    "scores": {
      "color_harmony": N,
      "fit": N,
      "proportion": N,
      "line_harmony": N,
      "style_coherence": N,
      "occasion_match": N
    },
    "gap_analysis": [
      {
        "dimension": "color_harmony|fit|proportion|line_harmony|style_coherence|occasion_match",
        "current": N,
        "target": N,
        "priority": 1,
        "actionable": "acción concreta y específica"
      }
    ]
  }
}

` + diagnosisScoringAddendum

const diagnosisScoringAddendum = `SCORING Y GAP ANALYSIS:

Tienes el análisis de imagen (Fase 1) con scores parciales y el perfil
del usuario (Fase 2) con sus deseos y contexto.

1. CONFIRMA O AJUSTA los scores de Fase 1 considerando el contexto:
   - Si el usuario quiere "trabajo" y el outfit es casual, occasion_match baja
   - Si el usuario quiere "refinar" y el outfit ya está cerca, style_coherence sube
   - color_harmony, fit, proportion, line_harmony: ajusta si el contexto lo amerita

2. ASIGNA score de style_coherence (1-10):
   - ¿Las piezas "conversan" entre sí?
   - ¿Hay una intención estética unificada?
   - ¿O es una colección aleatoria de prendas?

3. ASIGNA score de occasion_match (1-10):
   - ¿El outfit comunica lo correcto para la ocasión declarada?
   - ¿El nivel de formalidad es apropiado?

4. CALCULA target para cada dimensión:
   - Para "refinar": target = current + 2 a 3 puntos (mejora incremental)
   - Para "explorar": target = 8 mínimo (cambio más ambicioso)
   - Nunca target > 9 (perfección es irreal)

5. GENERA gap_analysis ordenado por prioridad:
   Prioridad = gap × peso de la dimensión
   Pesos: fit(0.25), color(0.20), coherence(0.15), occasion(0.15), proportion(0.15), lines(0.10)
   Cada GapItem debe tener una acción ESPECÍFICA y CONCRETA.

6. DETERMINA desired_archetype:
   - Si el usuario mencionó un referente, mapéalo al arquetipo más cercano
   - Si no, infiere del contexto (ocasión + proyección deseada)

REGLAS PARA ACCIONES:
- Cada acción debe respetar la temporada de color del usuario
- Cada acción debe respetar la familia Kibbe del usuario
- Cada acción debe mover hacia el arquetipo deseado
- Ser específico: "camisa Oxford verde bosque en relaxed fit" no "mejor camisa"
- Si confidence de color o kibbe es "low", dar recomendaciones más generales

CONFIANZA DEL ANÁLISIS:

Si algún score viene con confidence "low" (por foto con poca luz,
cuerpo parcial, o desenfoque):
- No confíes en ese score. Usa la información de la conversación.
- Asigna scores conservadores (6-7) para dimensiones sin datos fiables.
- SIEMPRE indica en el diagnóstico qué dimensiones tienen confianza reducida.
- Si hay blind spots no compensados, sé explícito en los gaps.

Responde en JSON con la estructura completa del profile.`
