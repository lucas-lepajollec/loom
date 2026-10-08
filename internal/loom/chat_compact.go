package loom

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Compactage du contexte, façon Hermes Agent : au lieu de vider la conversation
// quand la fenêtre de contexte se remplit, on la scinde en trois zones —
//
//	Head  (tête)  : messages système + tout premier message utilisateur. Protégé.
//	Tail  (queue) : les tours récents (dans un budget de tokens). Protégé.
//	Torso (torse) : tout le milieu. C'est LA SEULE zone compactée.
//
// Le torse est d'abord dégraissé sans IA (les vieux résultats d'outils longs
// sont remplacés par un marqueur), puis résumé par le modèle local en UN seul
// appel, et le tout est remplacé par un court résumé. Résultat : des
// conversations quasi illimitées sans jamais « clear », comme Hermes.
//
// La logique vit côté serveur (dans le flux de chat) donc elle profite à TOUS
// les clients — UI web, terminal, accès distant loom.local — sans duplication.

const (
	// Seuil de déclenchement proactif : on compacte quand l'historique estimé
	// dépasse cette fraction de la fenêtre de contexte.
	compactTriggerFrac = 0.75
	// Budget de la queue : fraction de la fenêtre gardée intacte (tours récents).
	// Plus la queue est petite, plus on compacte de torse d'un coup → le contexte
	// retombe bas et met longtemps à re-déclencher (au lieu de compacter souvent).
	compactTailFrac = 0.25
	// Un résultat d'outil du torse plus long que ça est remplacé par un marqueur
	// dans le torse DÉGRAISSÉ (repli si le résumé échoue).
	compactToolPruneLen = 200
	// Longueur à laquelle on RACCOURCIT (sans l'effacer) un résultat d'outil avant
	// de le donner au résumeur : assez pour que les faits d'une page web y soient,
	// assez court pour que dix pages tiennent dans la transcription.
	compactToolSummaryLen = 1200
)

// compactPrunedMarker remplace un vieux résultat d'outil dans le torse. Il dit
// EXPLICITEMENT de ne pas relancer l'outil : le texte précédent (« Old tool
// result cleared ») se lisait comme une invitation à re-télécharger la page, et
// le modèle repartait en boucle — page relue, contexte plein, nouveau compactage,
// résultat re-effacé, et ainsi de suite.
const compactPrunedMarker = "[Old tool result removed to save context. The important content is in the summary above — do NOT call this tool again to fetch it back.]"

// compactSummaryPrefix ouvre le message `user` synthétique qui porte le résumé.
// Sert aussi à le reconnaître pour ne pas le confondre avec une vraie demande.
const compactSummaryPrefix = "[CONTEXT COMPACTED]"

// compactEnabled indique si le compactage automatique du contexte est actif.
// Défaut : true. Seule une valeur off/false/0/no/non explicite (config.env
// COMPACT) le désactive.
func compactEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(ReadConfig()["COMPACT"])) {
	case "off", "false", "0", "no", "non":
		return false
	}
	return true
}

// ctxWindow renvoie la fenêtre de contexte vraiment utilisée (CTX= ou GGUF).
// 32768 n'est qu'un dernier recours pour le compactage si rien n'est lisible.
func ctxWindow() int {
	if n := effectiveCtx(nil); n > 0 {
		return n
	}
	return 32768
}

// msgText extrait le texte d'un message (Content est `any`, en pratique string
// ou nil quand l'assistant n'a que des tool_calls). Un message multimodal
// (userMessageContent : parties texte + image quand la vision est active) porte
// un tableau de parties ; on en recolle les segments `text` pour que l'estimation
// de contexte et le transcript de compaction ne partent pas d'un texte vide.
func msgText(m Message) string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []map[string]any:
		var b strings.Builder
		for _, part := range v {
			if part["type"] == "text" {
				if s, ok := part["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	case []any: // même contenu relu depuis le JSON persisté (map générique)
		var b strings.Builder
		for _, p := range v {
			if part, ok := p.(map[string]any); ok && part["type"] == "text" {
				if s, ok := part["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	return ""
}

// msgTokens estime grossièrement le coût en tokens d'un message (~4 caractères
// par token, plus un forfait par message pour le rôle et les délimiteurs). C'est
// volontairement approximatif : le comptage EXACT vient de llama.cpp
// (PromptTokensTotal) ; ici on veut juste décider quand compacter.
func msgTokens(m Message) int {
	n := 4
	n += len(msgText(m)) / 4
	for _, tc := range m.ToolCalls {
		n += (len(tc.Function.Name) + len(tc.Function.Arguments)) / 4
	}
	return n
}

// estimateTokens estime la taille de l'historique en tokens.
func estimateTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += msgTokens(m)
	}
	return total
}

// MaybeCompact compacte l'historique si (et seulement si) il dépasse le seuil
// proactif. Renvoie l'historique (compacté ou inchangé) et un booléen indiquant
// s'il a changé. À appeler sur l'historique BRUT (avant InjectSkills) pour que
// le résultat puisse être renvoyé au client sans le préfixe système injecté.
//
// knownTokens = taille RÉELLE du contexte au tour précédent (usage.prompt_tokens
// + tokens générés), telle que rapportée par llama.cpp et affichée par l'UI. On
// la préfère à estimateTokens() car cette dernière n'est qu'une heuristique et,
// surtout, ne « voit » pas le prompt système injecté (machine briefing) ni le
// gabarit de chat — donc elle sous-estime largement le vrai contexte. 0 = inconnu
// (clients sans compteur, ex. terminal) → repli sur l'estimation.
// compactWouldTrigger indique si un tour VA déclencher une compaction proactive
// (compactage activé ET contexte au-dessus du seuil). Exposé pour que le flux de
// chat puisse afficher une bannière « compactage en cours » AVANT de lancer le
// résumé (qui bloque plusieurs secondes), au lieu d'une UI figée sans info.
func compactWouldTrigger(msgs []Message, knownTokens int) bool {
	if !compactEnabled() {
		return false
	}
	used := knownTokens
	if used <= 0 {
		used = estimateTokens(msgs)
	}
	return used >= int(float64(ctxWindow())*compactTriggerFrac)
}

// logCompact trace UNE ligne par décision de compaction sur la sortie d'erreur
// (donc dans `journalctl -u loom-ui`). Sans ça, une compaction qui ne se
// déclenche pas — ou qui se déclenche et n'enlève rien — est invisible : côté
// UI on ne voit qu'une jauge qui reste haute, sans savoir si le seuil n'a pas
// été atteint ou si la réduction a été refusée.
func logCompact(phase string, used int, before, after []Message, changed bool) {
	fmt.Fprintf(os.Stderr, "[compact] %s ctx=%d threshold=%d/%d est_before=%d est_after=%d msgs=%d→%d changed=%v\n",
		phase, used, int(float64(ctxWindow())*compactTriggerFrac), ctxWindow(),
		estimateTokens(before), estimateTokens(after), len(before), len(after), changed)
}

func MaybeCompact(ctx context.Context, msgs []Message, caps Caps, knownTokens int) ([]Message, bool) {
	if !compactWouldTrigger(msgs, knownTokens) {
		return msgs, false
	}
	return compactMessages(ctx, msgs, caps)
}

// compactMessages exécute la compaction sans tenir compte du seuil (utilisé en
// secours réactif quand llama-server refuse un prompt trop long). Renvoie
// l'historique compacté et true s'il a effectivement changé.
// compactBounds calcule les frontières head/tail pour un historique donné et un
// budget de queue (en tokens). Fonction pure (pas d'IO) → testable :
//   - head : nb de messages protégés en tête = messages système + 1er message
//     utilisateur (il ancre l'objectif). Un message user est une frontière sûre.
//   - tailStart : index de début de la queue protégée. On remonte depuis la fin
//     jusqu'à remplir le budget, puis on recule jusqu'à une frontière SÛRE :
//     un message `user`, ou un `assistant` (qui, s'il porte des tool_calls, part
//     dans la queue AVEC ses résultats). On ne sépare ainsi jamais un
//     assistant+tool_calls de ses `tool`, et on ne laisse jamais un `tool`
//     orphelin en tête de queue.
//
// Reculer jusqu'à un `user` UNIQUEMENT était trop strict et rendait toute
// 2ᵉ compaction inopérante dans un même tour : pendant une longue boucle
// d'outils il n'y a AUCUN message `user`, donc la queue avalait toute la
// séquence d'outils et le torse était vide (« le compactage ne fait rien »
// alors que ce sont précisément les pages web lues qui remplissent la
// fenêtre). S'arrêter sur un `assistant` coupe proprement entre deux groupes
// d'appels d'outils.
//
// Le torse à compacter est [head, tailStart). Il est vide (tailStart <= head)
// quand il n'y a rien à résumer.
func compactMessages(ctx context.Context, msgs []Message, caps Caps) ([]Message, bool) {
	out, _, changed, _ := compactMessagesWith(ctx, msgs, ctxWindow(), summarizeTranscript)
	return out, changed
}

func renderTranscript(msgs []Message) string { return renderTranscriptWindow(msgs, ctxWindow()) }

// summarizeResp modélise le sous-ensemble utile d'une réponse non-streamée de
// /v1/chat/completions.
type summarizeResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// summarizeTranscript demande au modèle local un résumé dense et fidèle du torse.
// Un seul appel NON streamé, sans outils — comme Hermes, on réutilise le modèle
// principal déjà chargé (aucune dépendance, cohérent avec la fenêtre de contexte).
func summarizeTranscript(ctx context.Context, transcript string) (string, error) {
	return summarizeTranscriptAt(ctx, transcript, engineBase()+"/v1/chat/completions", engineAPIKey(), engineRequestModel(), true)
}

func summarizeTranscriptAt(ctx context.Context, transcript, endpoint, key, model string, local bool) (string, error) {
	sys := `You are a context compactor. You are given the transcript of the older turns of a conversation between a user and an AI assistant (with its tools). The PURPOSE of your summary is to let the conversation continue in a fresh, smaller context WITHOUT losing any information that is useful or important to understand what came before and keep working — preserve everything that matters, drop only what is redundant.

The assistant is MID-TASK: it will read your summary and must resume exactly where it left off, WITHOUT redoing work it has already done. Its own internal reasoning is NOT part of the transcript and is lost — your summary is the only memory it keeps.

Summarize densely and faithfully, keeping ONLY the essentials:
- The user's CURRENT request, goal(s) and constraints
- FINDINGS: the concrete information already gathered — facts, figures, dates, names, URLs, file paths, values, config. This is the most important part: whatever is not here is lost and will have to be looked up again.
- Sources already consulted (URLs opened, files read, commands run) — so they are not consulted a second time
- Decisions made and established facts
- STATE OF PROGRESS: what is already answered, what is still missing, and the next concrete step
Strict rules: no preamble or conclusion, no verbatim or long quotes, no throwaway detail. Use short bullet points. Aim for 300 words MAX — this is a compression summary, not a report.
Write the summary in the SAME language as the conversation.`

	payload := map[string]any{
		"model": model,
		"messages": []Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: transcript},
		},
		"stream": false,
		// Borne dure : sans ça, un modèle bavard (surtout à reasoning) produit un
		// résumé énorme et lent, donc peu de réduction → re-compaction à chaque tour.
		"max_tokens": 700,
	}
	if local {
		payload["temperature"] = 0.2
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	var out summarizeResp
	if err := brainModelPOST(ctx, endpoint, key, payload, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("summary: empty response")
	}
	c := out.Choices[0].Message.Content
	// Certains modèles à raisonnement préfixent un bloc <think>…</think> : on ne
	// garde que la réponse finale.
	if i := strings.LastIndex(c, thinkClose); i >= 0 {
		c = c[i+len(thinkClose):]
	}
	c = strings.TrimSpace(c)
	// Garde-fou dur : même si le modèle ignore la consigne de longueur, on tronque
	// pour garantir une vraie compression. 2200 caractères (≈ 550 tokens) et pas
	// 1500 : le résumé doit désormais porter les FAITS déjà trouvés, pas seulement
	// l'intention, sinon l'IA repart en recherche après chaque compactage. Coupé
	// sur une frontière de rune (é, … ne doivent pas devenir des �).
	if r := []rune(c); len(r) > 2200 {
		c = strings.TrimSpace(string(r[:2200])) + " […]"
	}
	return c, nil
}
