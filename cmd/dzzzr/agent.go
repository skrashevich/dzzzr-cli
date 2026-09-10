package main

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func init() {
	register(command{
		Name:  "agent",
		Usage: "agent [-security readonly|approve|full] ЗАПРОС...",
		Auth:  authNone,
		Run:   cmdAgent,
		Help:  "Выполнить запрос на естественном языке: модель работает инструментами движка",
	})
}

// agentOutput is the -json form of a finished run.
type agentOutput struct {
	Answer string `json:"answer"`
	Turns  int    `json:"turns"`
}

// cmdAgent runs one request and returns when the model has answered.
//
// The answer goes to stdout and everything else — the tools it called, the
// progress of the run, the report — to stderr, so the command can be piped
// into something that only wants the answer.
func cmdAgent(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		return fatal("укажите запрос: dzzzr agent «что у нас с уровнем»")
	}

	agentCfg, err := llmConfig()
	if err != nil {
		return err
	}
	if err := agentAuthorize(ctx, cfg, c); err != nil {
		return err
	}
	catalog, err := newAgentCatalog(cfg, c, &terminalConfirmer{cfg: cfg})
	if err != nil {
		return err
	}

	files := agentFileTools(cfg)
	fmt.Fprintf(cfg.stderr, "dzzzr agent %s: модель %s, инструментов %d, права %s, город %s\n",
		version, agentCfg.Model, len(catalog.Tools())+len(files), catalog.Policy(), cfg.city)

	var answer string
	res, err := agentloop.Run(ctx, agentCfg, &agentloop.RunInput{
		Catalog:      catalog,
		Extra:        files,
		Messages:     []agentloop.Message{{Role: agentloop.RoleUser, Content: prompt}},
		SystemPrompt: agentSystemPrompt(cfg, catalog, len(files) > 0),
	}, agentloop.Callbacks{
		OnEvent: func(ev agentloop.Event) {
			switch ev.Type {
			case agentloop.EventAssistantText:
				answer = ev.Text
				if !cfg.jsonOut {
					fmt.Fprintln(cfg.stdout, formatMarkdownForTerminal(ev.Text))
				}
			case agentloop.EventToolStart:
				fmt.Fprintf(cfg.stderr, "· %s %s\n", ev.ToolName, ev.ToolArgs)
			case agentloop.EventToolDone:
				cfg.debugf("%s: результат %d байт, ошибка=%t", ev.ToolName, len(ev.ToolResult), ev.ToolError)
				if ev.ToolError {
					cfg.debugf("%s: %s", ev.ToolName, ev.ToolResult)
				}
			case agentloop.EventReport:
				fmt.Fprintln(cfg.stderr, strings.TrimRight(ev.Report, "\n"))
			case agentloop.EventWarning:
				fmt.Fprintln(cfg.stderr, ev.Message)
			}
		},
		OnStatus: func(phase, message string) {
			// Progress is worth showing; the pricing lookup and other
			// bookkeeping is only worth showing when asked for.
			if phase == "debug" {
				cfg.debugf("%s", message)
				return
			}
			fmt.Fprintln(cfg.stderr, message)
		},
	})
	if err != nil {
		return fatal("%v", err)
	}
	if cfg.jsonOut {
		return outputJSON(cfg, agentOutput{Answer: answer, Turns: res.Turns})
	}
	return nil
}

// terminalConfirmer asks the user about a mutating tool call on the terminal.
// It writes to stderr because stdout carries the answer.
type terminalConfirmer struct{ cfg *config }

// ConfirmToolCall implements agenttools.Confirmer. Anything but an explicit
// yes is a refusal: a submitted code cannot be taken back.
func (t *terminalConfirmer) ConfirmToolCall(_ context.Context, req agenttools.ConfirmRequest) (bool, error) {
	fmt.Fprintf(t.cfg.stderr, "\nАгент хочет выполнить %s%s\nРазрешить? [y/N] ",
		req.Tool, encodeConfirmArgs(req.Args))
	line, err := t.cfg.lineReader().ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, fmt.Errorf("подтверждение не получено: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "д", "да":
		return true, nil
	default:
		return false, nil
	}
}

// encodeConfirmArgs renders a call's arguments for the confirmation prompt.
// The keys are sorted so the same call always reads the same way.
func encodeConfirmArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, k := range slices.Sorted(maps.Keys(args)) {
		parts = append(parts, fmt.Sprintf("%s=%v", k, args[k]))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
