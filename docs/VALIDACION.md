# Plan de validación — guaperrimo.ai

## Lo que queremos validar

> Un hombre que no sabe de moda, con un evento o momento concreto, confía en
> lo que le recomienda la app lo suficiente como para ir a comprarlo.

El agente, el try-on y las tiendas cercanas son medios para eso, no el objetivo.

## Supuestos, en orden

Cada uno depende del anterior; no tiene caso optimizar el 3 si falla el 1.

| # | Supuesto | Cómo se mide |
|---|---|---|
| 1 | **Lo usan.** Termina la foto y la conversación sin irse a la mitad. | Porcentaje que llega a la recomendación. |
| 2 | **Le cree.** La recomendación le parece acertada para él y su evento, no genérica. | "¿Te pondrías esto?" al final; porcentaje que guarda la lista. |
| 3 | **Actúa.** Toca "Comprar" o "Cómo llegar", va a la tienda y compra al menos una prenda. | Seguimiento a la semana: "¿compraste algo de la lista?". |
| 4 | **Alguien paga.** Usuario por sesión o suscripción, o tiendas por comisión. | Disposición a pagar en la entrevista; el supuesto 3 dice si hay algo que cobrar. |

### Hipótesis aparte: el try-on

> Verse con la ropa puesta aumenta la confianza para comprar.

Es atractivo y vende en una demo, pero cuesta dinero por imagen. Se mide, no se asume.

## Experimento

- **Participantes:** 10 a 20 hombres reales con un evento próximo (boda, entrevista,
  primera cita). Amigos de amigos, no gente que quiera darnos gusto.
- **Sesión:** usan la app enfrente o por videollamada, sin ayuda. Anotar dónde se traban.
- **Al terminar, tres preguntas:**
  1. ¿Te pondrías esto?
  2. ¿Vas a comprar algo?
  3. ¿Cuánto pagarías por esto?
- **Una semana después:** "¿Compraste algo de la lista?". Esta respuesta vale más que todas las demás.
- **Try-on:** a la mitad se le muestran las imágenes y a la otra mitad no. Si los que lo
  vieron confían o compran claramente más, el try-on vale lo que cuesta.

## Registro por participante

| Participante | Evento | Terminó (1) | ¿Se lo pondría? (2) | Guardó lista (2) | ¿Va a comprar? | ¿Cuánto pagaría? (4) | Vio try-on | Compró a la semana (3) | Notas |
|---|---|---|---|---|---|---|---|---|---|
| | | | | | | | | | |

## Presupuesto y duración

- 2 a 3 semanas.
- Unos 20 a 30 USD en total entre FASHN (~0.075 USD por imagen) y SerpAPI.
- Topes del server: 4 imágenes de try-on por sesión y 10 por día (`TRYON_MAX_PER_SESSION`, `TRYON_MAX_PER_DAY`).

## Lo que NO validamos ahora

Calidad de un try-on propio, escala, latencia, ni un feed de inventario de tiendas
físicas. Todo eso importa solo si el supuesto 3 sale que sí.

## Criterio para seguir

Seguir invirtiendo si al menos la mitad dice que se pondría lo recomendado y al menos
2 o 3 de cada 10 compran algo de la lista en la semana siguiente. Si no, revisar qué
supuesto falló antes de construir más.
