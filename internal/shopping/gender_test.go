package shopping

import "testing"

func TestIsMenswear(t *testing.T) {
	for title, want := range map[string]bool{
		"Camisa de lino manga larga para hombre":   true,
		"Pantalón de vestir slim fit caballero":    true,
		"Chamarra bomber Hombre H&M":               true,
		"Blusa de lino manga larga":                false,
		"Camisa de lino para Mujer":                false,
		"Camisa Oxford Dama Slim":                  false,
		"Vestido camisero de algodón":              false,
		"Pantalón chino para niño":                 false,
		"Playera básica Women's":                   false,
		"Top crop de punto":                        false,
		"Regalo para ella: pañuelo":                false,
		"Saco de lino Damasco (tono)":              true, // "damasco" is not "dama"
		"Camisa de mezclilla unisex":               true,
		"Pantalón de vestir Yale Regular Fit 7358": true,
	} {
		if got := IsMenswear(title); got != want {
			t.Errorf("IsMenswear(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestMensQuery(t *testing.T) {
	for in, want := range map[string]string{
		"camisa lino verde":         "camisa lino verde hombre",
		"camisa lino verde hombre":  "camisa lino verde hombre",
		"pantalón vestir caballero": "pantalón vestir caballero",
		"Zara blazer azul marino":   "Zara blazer azul marino hombre",
	} {
		if got := MensQuery(in); got != want {
			t.Errorf("MensQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
