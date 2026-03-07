package banner

import (
	"fmt"
)

func Print() {
	// Titanium Silk Border (#A1A1AA)
	border := "\033[38;2;161;161;170m"

	// Claude Slate Main (#818AA3)
	main := "\033[38;2;129;138;163m"
	reset := "\033[0m"

	banner := `
  ` + border + `╔══[` + main + ` SKOVENET TERMINAL ` + border + `]══════════════════════════════════════════════════════╗` + `
  ` + border + `║` + main + `                                                                             ` + border + `║` + `
  ` + border + `║` + main + `  ███████╗██╗  ██╗ ██████╗ ██╗   ██╗███████╗   ███╗   ██╗███████╗████████╗   ` + border + `║` + `
  ` + border + `║` + main + `  ██╔════╝██║ ██╔╝██╔═══██╗██║   ██║██╔════╝   ████╗  ██║██╔════╝╚══██╔══╝   ` + border + `║` + `
  ` + border + `║` + main + `  ███████╗█████╔╝ ██║   ██║██║   ██║█████╗     ██╔██╗ ██║█████╗     ██║      ` + border + `║` + `
  ` + border + `║` + main + `  ╚════██║██╔═██╗ ██║   ██║╚██╗ ██╔╝██╔══╝     ██║╚██╗██║██╔══╝     ██║      ` + border + `║` + `
  ` + border + `║` + main + `  ███████║██║  ██╗╚██████╔╝ ╚████╔╝ ███████╗   ██║ ╚████║███████╗   ██║      ` + border + `║` + `
  ` + border + `║` + main + `  ╚══════╝╚═╝  ╚═╝ ╚═════╝   ╚═══╝  ╚══════╝   ╚═╝  ╚═══╝╚══════╝   ╚═╝      ` + border + `║` + `
  ` + border + `║` + main + `                                                                             ` + border + `║` + `
  ` + border + `╠══[` + main + ` STATUS ` + border + `]═════════════════════════════════════════════════════════════════╣` + `
  ` + border + `║` + main + `  NODE: SK-01  ·  PING: 1ms  ·  UPTIME: 99.9%  ·  SIGNAL: ████████░░ 80%     ` + border + `║` + `
  ` + border + `╚══[` + main + ` v1.1 ` + border + `]════════════════════════════════════════════════[` + main + ` SECURE · ONLINE ` + border + `]╝` + `
`
	fmt.Print(banner)
	fmt.Print(reset)
	fmt.Println()
}
