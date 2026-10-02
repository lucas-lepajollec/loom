package loom

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

// cmdChat is the interactive terminal chat loop.
// First positional arg (if any) becomes the system prompt.
func cmdChat(args []string) error {
	if !healthCheck() {
		return fmt.Errorf("server unreachable on :%d — run loom start first", LLMPort())
	}
	sysPrompt := strings.Join(args, " ")
	msgs := []Message{}
	if sysPrompt != "" {
		msgs = append(msgs, Message{Role: "system", Content: sysPrompt})
	}
	fmt.Printf("\n%s  —  /reset to clear, /sys <prompt> to change the system prompt, /quit or Ctrl-D to exit\n", cyan("loom chat"))
	if sysPrompt != "" {
		fmt.Println(dim("system: " + sysPrompt))
	}
	fmt.Println()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for {
		fmt.Print(bold("you") + " > ")
		if !sc.Scan() {
			fmt.Println()
			return nil
		}
		user := sc.Text()
		u := strings.TrimSpace(user)
		if u == "" {
			continue
		}
		if u == "/quit" || u == "/exit" {
			return nil
		}
		if u == "/reset" {
			kept := []Message{}
			for _, m := range msgs {
				if m.Role == "system" {
					kept = append(kept, m)
				}
			}
			msgs = kept
			fmt.Println(dim("[context cleared]"))
			continue
		}
		if strings.HasPrefix(u, "/sys ") {
			newSys := strings.TrimSpace(u[5:])
			msgs = msgs[:0]
			if newSys != "" {
				msgs = append(msgs, Message{Role: "system", Content: newSys})
			}
			fmt.Println(dim("[system prompt updated]"))
			continue
		}
		msgs = append(msgs, Message{Role: "user", Content: user})
		full := strings.Builder{}
		inReason := false
		var stats *StatsEvent
		// Print the assistant prefix once; reasoning is shown inline with a tag.
		fmt.Print(cyan("loom") + " > ")
		caps := globalCaps()
		// Compaction proactive (façon Hermes) : on résume les vieux tours quand
		// l'historique dépasse le seuil, au lieu d'imposer un /reset.
		if compacted, changed := MaybeCompact(context.Background(), msgs, caps, 0); changed {
			msgs = compacted
			fmt.Println(dim("[context compacted to fit the window]"))
		}
		extra, err := runChat(context.Background(), InjectSkills(msgs, caps), 0.7, caps, func(ev StreamEvent) bool {
			switch {
			case ev.Err != nil:
				fmt.Printf("\n%s\n", red("[error] "+ev.Err.Error()))
			case ev.NewHistory != nil:
				// Compaction survenue pendant le tour : elle REMPLACE l'historique (elle
				// contient déjà le tour en cours), préfixe système injecté retiré. Sans
				// ça le terminal repartirait du fil complet au tour suivant.
				base := ev.NewHistory
				for len(base) > 0 && base[0].Role == "system" {
					base = base[1:]
				}
				msgs = append([]Message(nil), base...)
				fmt.Println(dim("\n[context compacted to fit the window]"))
			case ev.Stats != nil:
				stats = ev.Stats
			case ev.DropReasoning:
				// Le tour a « pensé sans agir » : on relance. Impossible d'effacer le
				// texte déjà imprimé en terminal — on referme juste la ligne reasoning.
				if inReason {
					fmt.Print("\n")
					inReason = false
				}
			case ev.ToolUsed != nil:
				if ev.ToolUsed.Done || ev.ToolUsed.Typing {
					break // résultat / frappe live affichés côté web ; en terminal on garde l'annonce seule
				}
				icon := "🧠"
				verb := "memory"
				switch ev.ToolUsed.Name {
				case "bash":
					icon = "⚙️"
					verb = "execution"
				case "write":
					icon = "📄"
					verb = "writing"
				case "edit":
					icon = "✏️"
					verb = "editing"
				case "web_search", "web_open", "web_read", "web_grep":
					icon = "🌐"
					verb = "web"
				}
				if inReason {
					fmt.Print("\n")
					inReason = false
				}
				fmt.Printf("\n%s %s : %s\n%s ", dim(icon+" "+verb), "", magenta(ev.ToolUsed.Label), cyan("loom")+" >")
			case ev.Reasoning != "":
				if !inReason {
					fmt.Print(magenta("[reasoning] ") + dim(""))
					inReason = true
				}
				fmt.Print(dim(ev.Reasoning))
			case ev.Content != "":
				if inReason {
					fmt.Print("\n" + cyan("loom") + " > ")
					inReason = false
				}
				full.WriteString(ev.Content)
				fmt.Print(ev.Content)
			}
			return true
		})
		if inReason {
			fmt.Println()
		}
		fmt.Println()
		if stats != nil {
			fmt.Printf("%s prefill %d tok · %.0f tok/s   decode %d tok · %.1f tok/s\n",
				dim("→"), stats.PromptTokens, stats.PromptPerSecond, stats.GenTokens, stats.GenPerSecond)
		}
		fmt.Println()
		if err == nil {
			// Persist the tool turns (skill reads, shell runs) BEFORE the final
			// answer, so next turn the model remembers it already did them instead
			// of re-invoking the same skill/command from scratch.
			msgs = append(msgs, extra...)
			msgs = append(msgs, Message{Role: "assistant", Content: full.String()})
		}
	}
}
