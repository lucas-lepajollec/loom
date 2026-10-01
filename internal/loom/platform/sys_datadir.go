package platform

// defaultConfig est la configuration de départ d'une installation neuve. Les
// valeurs sont volontairement incomplètes (BIN et MODEL sont vides) : c'est
// l'écran d'accueil, ou « loom llamacpp install », qui les renseigne.
func DefaultConfig() map[string]string {
	return map[string]string{
		"PORT":   "8081",
		"HOST":   "127.0.0.1",
		"BATCH":  "2048",
		"UBATCH": "512",
		"NGL":    "999",
	}
}
