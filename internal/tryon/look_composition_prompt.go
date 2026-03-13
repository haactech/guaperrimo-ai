package tryon

const lookCompositionPrompt = `Eres un estilista de moda experto. Tu tarea es componer %d looks completos para el usuario basándote en su perfil de estilo y las acciones prioritarias de mejora.

## Perfil del usuario
%s

## Acciones prioritarias
%s

## Reglas
1. Cada look debe tener exactamente 2 prendas: una "upper_body" y una "lower_body"
2. Respeta la temporada de color del usuario (solo colores que armonicen)
3. Respeta la familia Kibbe del usuario (siluetas y proporciones adecuadas)
4. Cada look debe tener un vibe/estilo distinto (ej: casual elevado, smart casual, urbano refinado)
5. Las descripciones de cada pieza deben ser suficientemente específicas para buscar en un catálogo de moda masculina
6. El campo "category" debe ser exactamente "upper_body" o "lower_body"
7. Asigna un ID único a cada look: "look_1", "look_2", etc.

## Formato de respuesta
Responde ÚNICAMENTE con JSON válido, sin texto adicional:

{
  "looks": [
    {
      "id": "look_1",
      "name": "Nombre del look",
      "description": "Descripción breve del look completo",
      "vibe": "casual elevado",
      "pieces": [
        {
          "slot": "upper_body",
          "description": "Camisa de lino azul marino con cuello italiano",
          "category": "upper_body",
          "reasoning": "El azul marino armoniza con su temporada Verano y la estructura del cuello complementa su silueta Natural"
        },
        {
          "slot": "lower_body",
          "description": "Pantalón chino slim en beige arena",
          "category": "lower_body",
          "reasoning": "El beige arena es un neutro cálido que balancea el azul y el corte slim mantiene proporciones limpias"
        }
      ]
    }
  ]
}`
