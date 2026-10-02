package loom

import (
	"fmt"
)

// Le « mode agent » est l'unique interrupteur qui donne à l'IA l'accès à ses
// outils : le shell (run_shell), l'écriture de fichiers et sa mémoire
// (mem_search/mem_read/mem_add/mem_edit).

func agentEnabled() bool { return getBool(bkState, "agent") }

func setAgentEnabled(on bool) error { return putBool(bkState, "agent", on) }

func cmdAgent(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "on":
		if err := setAgentEnabled(true); err != nil {
			return err
		}
		fmt.Println(green("[ok]") + " agent mode enabled — the AI has full shell access (" + shellName() + "), file writing and its memory")
	case "off":
		if err := setAgentEnabled(false); err != nil {
			return err
		}
		fmt.Println(green("[ok]") + " agent mode disabled")
	case "", "status":
		state := dim("off")
		if agentEnabled() {
			state = green("on")
		}
		fmt.Printf("%s  state: %s\n", cyan("Agent mode"), state)
		fmt.Printf("  tools: %s (timeout %ds, max %ds) + write/edit + memory (mem_search/mem_read/mem_add/mem_edit)\n", shellName(), toolDefaultTimeout, toolMaxTimeout)
		mem := MemList()
		if len(mem) == 0 {
			fmt.Printf("  memory: no pages — create %s/<name>.md\n", memoryDir())
			return nil
		}
		fmt.Printf("  memory (%s):\n", memoryDir())
		for _, p := range mem {
			fmt.Printf("    %s  %s\n", bold(p.Name), p.Title)
		}
	default:
		return fmt.Errorf("usage: loom agent [on|off|status]")
	}
	return nil
}
