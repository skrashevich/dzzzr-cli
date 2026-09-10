package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/skrashevich/dzzzr-cli/agentmcp"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func init() {
	register(command{Name: "mcp", Usage: "mcp [-security readonly|approve|full]", Auth: authNone, Run: cmdMCP,
		Help: "Отдавать инструменты движка по протоколу MCP через stdin/stdout"})
}

// cmdMCP serves the engine toolset over the Model Context Protocol.
//
// The command is registered as authNone so the server still starts without a
// session and can report what is missing to its client; with a saved session
// it plays as that team.
//
// stdout carries the protocol, so every message this command writes goes to
// stderr, including in -json mode.
func cmdMCP(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда mcp не принимает аргументов")
	}
	if err := agentAuthorize(ctx, cfg, c); err != nil {
		return err
	}

	// Under the approve policy every mutation is put to the MCP client first.
	catalog, err := newAgentCatalog(cfg, c, agentmcp.NewElicitConfirmer())
	if err != nil {
		return err
	}
	server, err := agentmcp.NewServer(catalog, version)
	if err != nil {
		return fatal("не удалось запустить сервер MCP: %v", err)
	}

	_, _ = fmt.Fprintf(cfg.stderr, "dzzzr mcp %s: инструментов %d, права %s, город %s\n",
		version, len(catalog.Tools()), catalog.Policy(), cfg.city)
	if !c.HasAdminCredentials() {
		_, _ = fmt.Fprintln(cfg.stderr, "Инструменты организатора выключены: не заданы -admin-login и -admin-password.")
	}
	if err := agentmcp.ServeStdio(ctx, server); err != nil && !endedByClient(err) {
		return fatal("сервер MCP завершился с ошибкой: %v", err)
	}
	return nil
}

// endedByClient reports whether the server stopped because the client went
// away: it closed stdin, or the user interrupted the process. The MCP SDK
// formats the transport's io.EOF into its own message with %v instead of %w,
// so the end of a session cannot be recognized by errors.Is alone.
func endedByClient(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || strings.HasSuffix(err.Error(), "EOF")
}
