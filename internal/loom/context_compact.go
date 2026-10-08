package loom

import (
	"context"
	"fmt"
	"strings"
)

func compactBounds(msgs []Message, tailBudget int) (head, tailStart int) {
	for head < len(msgs) && msgs[head].Role == "system" {
		head++
	}
	if head < len(msgs) && msgs[head].Role == "user" {
		head++
	}
	tailStart = len(msgs)
	acc := 0
	for i := len(msgs) - 1; i >= head; i-- {
		acc += msgTokens(msgs[i])
		tailStart = i
		if acc >= tailBudget {
			break
		}
	}
	for tailStart > head && msgs[tailStart].Role != "user" && msgs[tailStart].Role != "assistant" {
		tailStart--
	}
	return head, tailStart
}

func compactMessagesWith(ctx context.Context, msgs []Message, window int, summarize func(context.Context, string) (string, error)) ([]Message, string, bool, error) {
	// Budget de queue = fraction de la CONVERSATION (pas de la fenêtre). Le lier à
	// la fenêtre était le bug : une conversation de 25k tokens dans une fenêtre de
	// 64k gardait 16k (0.25×64k) en queue → torse minuscule → réduction < 20% →
	// refusée. Lié à la conversation, on garde toujours ~25% des tours récents et
	// on compacte les ~75% du début, quelle que soit la taille de la fenêtre.
	tailBudget := int(float64(estimateTokens(msgs)) * compactTailFrac)
	head, tailStart := compactBounds(msgs, tailBudget)

	// Rien à compacter : le torse [head, tailStart) est vide.
	if tailStart <= head {
		return msgs, "", false, nil
	}

	torso := msgs[head:tailStart]

	// 3. Dégraissage sans IA : les vieux résultats d'outils longs deviennent un
	//    marqueur. On travaille sur une copie pour ne pas muter l'historique amont.
	//    ⚠️ Ce torse dégraissé ne sert QUE de repli si le résumé échoue — surtout
	//    PAS d'entrée au résumeur, cf. juste en dessous.
	pruned := make([]Message, len(torso))
	for i, m := range torso {
		pruned[i] = m
		if m.Role == "tool" {
			if t := msgText(m); len(t) > compactToolPruneLen {
				pruned[i].Content = compactPrunedMarker
			}
		}
	}

	// 4. Résumé du torse par le modèle de la discussion (un seul appel).
	//
	//    Le résumé se fait sur le torse ORIGINAL, pas sur le dégraissé. C'était LE
	//    bug de fond : on effaçait tous les résultats d'outils PUIS on demandait un
	//    résumé de ce qui restait. Le résumeur ne voyait donc que « Tool result:
	//    [Old tool result cleared] » à la place de chaque page web lue — le résumé
	//    ne pouvait contenir AUCUNE des informations trouvées, seulement la trace
	//    que des outils avaient tourné. À chaque compactage, l'IA repartait donc
	//    d'une recherche vide et recommençait à zéro : elle ne s'arrêtait jamais.
	//
	//    Les résultats d'outils sont seulement RACCOURCIS (leur début, qui porte
	//    l'essentiel : titre, en-tête, premières lignes) pour que la transcription
	//    reste bornée. Les faits survivent, le volume reste maîtrisé.
	forSummary := make([]Message, len(torso))
	for i, m := range torso {
		forSummary[i] = m
		if m.Role == "tool" {
			if r := []rune(msgText(m)); len(r) > compactToolSummaryLen {
				forSummary[i].Content = string(r[:compactToolSummaryLen]) + "\n[…remainder cut off]"
			}
		}
	}
	summary, err := summarize(ctx, renderTranscriptWindow(forSummary, window))
	var mid []Message
	if err != nil || strings.TrimSpace(summary) == "" {
		mid = pruned
	} else {
		// Le résumé est injecté comme un tour utilisateur→assistant (jamais un
		// message `system` au milieu : certains gabarits, ex. Qwen, exigent que le
		// system soit uniquement en tête — cf. mémoire qwen36-chat-template-fix).
		mid = []Message{
			{Role: "user", Content: compactSummaryPrefix + " The earlier turns of this conversation were summarized to save context. Here is the summary:\n\n" + summary},
			{Role: "assistant", Content: "Understood. I'll resume from exactly where I left off, using the findings above, without redoing work that is already done."},
		}
	}

	// La demande EN COURS ne doit JAMAIS être diluée dans le résumé. Pendant une
	// longue boucle d'outils (recherche web : dix pages lues d'affilée), la queue
	// n'est faite que d'appels d'outils : le message `user` qui a lancé la
	// recherche tombe dans le torse, alors que le TOUT PREMIER message de la
	// conversation, lui, reste épinglé en tête. Après compaction le modèle voyait
	// donc, comme seule demande explicite, la question du DÉBUT de la conversation
	// — et il y répondait en abandonnant la recherche en cours.
	// On réinjecte donc textuellement la dernière vraie demande du torse, juste
	// avant la queue (les résultats d'outils qu'elle a produits la suivent, comme
	// dans l'historique d'origine). Le torse reste entièrement compactable.
	var pending []Message
	for i := len(torso) - 1; i >= 0; i-- {
		if torso[i].Role != "user" {
			continue
		}
		if strings.HasPrefix(msgText(torso[i]), compactSummaryPrefix) {
			continue // résumé d'une compaction précédente, pas une demande
		}
		pending = []Message{torso[i]}
		break
	}

	out := make([]Message, 0, head+len(mid)+len(pending)+len(msgs)-tailStart)
	out = append(out, msgs[:head]...)
	out = append(out, mid...)
	out = append(out, pending...)
	out = append(out, msgs[tailStart:]...)

	// Garantie de réduction : on n'accepte la compaction que si elle enlève au
	// moins ~20% du contexte estimé. Sinon (torse déjà maigre, résumé peu rentable)
	// on la refuse — sans ça, loom « compactait » à presque chaque message sans
	// vraiment réduire, puis re-déclenchait aussitôt.
	before, after := estimateTokens(msgs), estimateTokens(out)
	if after > before*4/5 {
		return msgs, "", false, err
	}
	return out, summary, true, err
}

// renderTranscript sérialise le torse en texte lisible pour le résumeur.
func renderTranscriptWindow(msgs []Message, window int) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			fmt.Fprintf(&b, "User: %s\n", msgText(m))
		case "assistant":
			if t := msgText(m); t != "" {
				fmt.Fprintf(&b, "Assistant: %s\n", t)
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "Assistant → tool %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
		case "tool":
			fmt.Fprintf(&b, "Tool result: %s\n", msgText(m))
		case "system":
			fmt.Fprintf(&b, "System: %s\n", msgText(m))
		}
	}
	s := b.String()
	// Garde-fou pour les petites fenêtres : le résumeur ne doit pas lui-même
	// déborder. On plafonne la transcription (~0,7×contexte en tokens ≈ 2,8
	// caractères/token) en gardant la FIN (la plus récente) et en marquant la
	// troncature de tête.
	maxChars := int(float64(window) * 2.8)
	if maxChars > 0 && len(s) > maxChars {
		s = "[…start truncated…]\n" + strings.ToValidUTF8(s[len(s)-maxChars:], "")
	}
	return s
}
