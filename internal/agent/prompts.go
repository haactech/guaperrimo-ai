package agent

import (
	"fmt"
	"strings"

	"stylerag/internal/llm"
	"stylerag/internal/session"
)

// PromptManager handles system prompts and knowledge base integration
type PromptManager struct {
	systemPrompt string
	knowledge    map[string]string // knowledge file name -> content
}

func NewPromptManager(knowledgeDir string) (*PromptManager, error) {
	pm := &PromptManager{
		systemPrompt: defaultSystemPrompt,
		knowledge:    make(map[string]string),
	}
	// TODO: Load knowledge files from knowledgeDir
	return pm, nil
}

func (pm *PromptManager) BuildMessages(profile *session.StyleProfile, userMessage string) []llm.Message {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: pm.buildSystemPrompt(profile)},
	}

	// Add conversation history from profile if available
	for _, msg := range profile.ConversationHistory {
		messages = append(messages, llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		})
	}

	// Add current user message
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userMessage,
	})

	return messages
}

func (pm *PromptManager) buildSystemPrompt(profile *session.StyleProfile) string {
	var sb strings.Builder
	sb.WriteString(pm.systemPrompt)

	if len(profile.CurrentStyle) > 0 {
		sb.WriteString(fmt.Sprintf("\n\nEstilo actual del usuario: %s", strings.Join(profile.CurrentStyle, ", ")))
	}
	if len(profile.TargetStyle) > 0 {
		sb.WriteString(fmt.Sprintf("\nEstilo objetivo: %s", strings.Join(profile.TargetStyle, ", ")))
	}
	if profile.Budget.Max > 0 {
		sb.WriteString(fmt.Sprintf("\nPresupuesto: $%.0f - $%.0f %s", profile.Budget.Min, profile.Budget.Max, profile.Budget.Currency))
	}
	if len(profile.Restrictions) > 0 {
		sb.WriteString(fmt.Sprintf("\nRestricciones: %s", strings.Join(profile.Restrictions, ", ")))
	}

	return sb.String()
}

const defaultSystemPrompt = `Eres un asesor de imagen personal experto en moda masculina smart casual. Tu objetivo es ayudar al usuario a mejorar su estilo de forma gradual y realista.

Principios:
1. Empatía primero: Entender la intención emocional detrás de cada petición
2. Gradualidad: No proponer cambios drásticos, sino evoluciones naturales del estilo actual
3. Practicidad: Recomendar prendas que combinen con lo que el usuario ya tiene
4. Contexto: Considerar ocasiones de uso (oficina, citas, salidas casuales)

Flujo de conversación:
1. Si el usuario sube una foto, analiza su estilo actual con respeto
2. Pregunta sobre sus objetivos y contexto de uso
3. Recopila preferencias (colores, fit, presupuesto, restricciones)
4. Presenta recomendaciones explicando el porqué de cada elección

Usa un tono cercano pero profesional. Evita jerga excesiva. Explica las reglas de estilo de forma accesible.`
