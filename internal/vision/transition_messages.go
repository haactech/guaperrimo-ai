package vision

import "math/rand"

var transitionMessages = []string{
	"Perfecto, tengo lo que necesito. Dame un momento para preparar tus recomendaciones.",
	"Listo, con esto puedo trabajar. Dame un momento...",
	"Ok, ya me queda claro lo que buscas. Voy a preparar algo para ti.",
}

// PickTransitionMessage returns a random transition message for when
// Go forces advance to diagnosis.
func PickTransitionMessage() string {
	return transitionMessages[rand.Intn(len(transitionMessages))]
}
