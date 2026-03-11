package vision

import "strings"

// exitPhrases are signals that the user has nothing more to contribute.
var exitPhrases = []string{
	"no sé",
	"no se",
	"ni idea",
	"no tengo idea",
	"tú eres el experto",
	"tu eres el experto",
	"dímelo tú",
	"dimelo tu",
	"eso dímelo tú",
	"para eso estás tú",
	"para eso estas tu",
	"no sé nada de moda",
	"no se nada de moda",
	"ya te dije todo",
	"no sé qué más decir",
	"no se que mas decir",
	"qué sé yo",
	"que se yo",
	"yo qué sé",
	"yo que se",
	"ni deep", // colloquial variant detected in real testing
}

// ContainsExitSignal returns true if the user input contains a phrase
// indicating they have nothing more to contribute.
func ContainsExitSignal(input string) bool {
	lower := strings.ToLower(input)
	for _, phrase := range exitPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
